package storage

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/gotd/td/tg"
	"tordown/internal/telegram"
)

const telegramPartSize = 512 * 1024 // 512KB

// TelegramStorage implements storage.ClientImpl for streaming to Telegram.
type TelegramStorage struct {
	tgClient *telegram.Client
	mu       sync.Mutex
	torrents map[metainfo.Hash]*telegramTorrent
}

func NewTelegramStorage(tgClient *telegram.Client) *TelegramStorage {
	return &TelegramStorage{
		tgClient: tgClient,
		torrents: make(map[metainfo.Hash]*telegramTorrent),
	}
}

type telegramTorrent struct {
	ts       *TelegramStorage
	infoHash metainfo.Hash
	mu       sync.Mutex
	pieces   map[int]*telegramPiece

	files         []*telegramFile
	filesByPart   map[int64]*telegramFile // partNum global -> file
}

type telegramFile struct {
	id         int64
	name       string
	offset     int64
	length     int64
	totalParts int
	
	// Track uploaded parts for this file
	uploadedParts map[int]bool
	committed     bool
	document      *tg.Document
}

func (ts *TelegramStorage) OpenTorrent(infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if t, ok := ts.torrents[infoHash]; ok {
		return t, nil
	}

	t := &telegramTorrent{
		ts:           ts,
		infoHash:     infoHash,
		pieces:       make(map[int]*telegramPiece),
		filesByPart:  make(map[int64]*telegramFile),
	}
	ts.torrents[infoHash] = t
	return t, nil
}

func (t *telegramTorrent) initFiles(torrent storage.Torrent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.files) > 0 {
		return
	}

	files := torrent.Files()
	for _, f := range files {
		var id int64
		binary.Read(rand.Reader, binary.LittleEndian, &id)

		tf := &telegramFile{
			id:            id,
			name:          f.DisplayPath(),
			offset:        f.Offset(),
			length:        f.Length(),
			totalParts:    int((f.Length() + telegramPartSize - 1) / telegramPartSize),
			uploadedParts: make(map[int]bool),
		}
		if tf.totalParts == 0 {
			tf.totalParts = 1
		}
		t.files = append(t.files, tf)
		
		// Map global offsets to this file's parts
		startPart := tf.offset / telegramPartSize
		endPart := (tf.offset + tf.length - 1) / telegramPartSize
		for p := startPart; p <= endPart; p++ {
			t.filesByPart[p] = tf
		}
	}
}

func (t *telegramTorrent) uploadPartForFile(tf *telegramFile, partNumInFile int, data []byte) error {
	raw := t.ts.tgClient.Raw()
	if raw == nil {
		return errors.New("telegram client not connected")
	}

	_, err := raw.UploadSaveBigFilePart(context.Background(), &tg.UploadSaveBigFilePartRequest{
		FileID:         tf.id,
		FilePart:       partNumInFile,
		FileTotalParts: tf.totalParts,
		Bytes:          data,
	})
	if err == nil {
		t.mu.Lock()
		tf.uploadedParts[partNumInFile] = true
		t.mu.Unlock()
	}
	return err
}

func (t *telegramTorrent) commitFile(tf *telegramFile) error {
	raw := t.ts.tgClient.Raw()
	if raw == nil {
		return errors.New("telegram client not connected")
	}

	var randomID int64
	binary.Read(rand.Reader, binary.LittleEndian, &randomID)

	_, err := raw.MessagesSendMedia(context.Background(), &tg.MessagesSendMediaRequest{
		Peer: &tg.InputPeerSelf{},
		Media: &tg.InputMediaUploadedDocument{
			File: &tg.InputFileBig{
				ID:    tf.id,
				Parts: tf.totalParts,
				Name:  tf.name,
			},
			MimeType: "application/octet-stream",
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{FileName: tf.name},
			},
		},
		Message:  fmt.Sprintf("Downloaded: %s", tf.name),
		RandomID: randomID,
	})
	if err == nil {
		tf.committed = true
		// Store the document info if returned
		if updates, ok := err.(*tg.Updates); ok {
			for _, u := range updates.Updates {
				if m, ok := u.(*tg.UpdateNewMessage); ok {
					if msg, ok := m.Message.(*tg.Message); ok {
						if media, ok := msg.Media.(*tg.MessageMediaDocument); ok {
							if doc, ok := media.Document.(*tg.Document); ok {
								tf.document = doc
							}
						}
					}
				}
			}
		}
	}
	return err
}

func (t *telegramTorrent) Stat() (storage.TorrentStat, error) {
	return storage.TorrentStat{}, nil
}

func (t *telegramTorrent) Close() error {
	return nil
}

func (t *telegramTorrent) Piece(p metainfo.Piece) storage.PieceImpl {
	t.mu.Lock()
	defer t.mu.Unlock()

	if pi, ok := t.pieces[p.Index()]; ok {
		return pi
	}

	pi := &telegramPiece{
		t:     t,
		piece: p,
	}
	t.pieces[p.Index()] = pi
	return pi
}

type telegramPiece struct {
	t     *telegramTorrent
	piece metainfo.Piece
}

func (p *telegramPiece) ReadAt(b []byte, off int64) (n int, err error) {
	return 0, io.EOF
}

func (p *telegramPiece) WriteAt(b []byte, off int64) (n int, err error) {
	t := p.t
	torrent := p.piece.Torrent()
	t.initFiles(torrent)

	globalOff := int64(p.piece.Index())*torrent.PieceLength() + off
	
	remaining := b
	currentOff := globalOff
	
	for len(remaining) > 0 {
		partNumGlobal := currentOff / telegramPartSize
		offInPart := currentOff % telegramPartSize
		
		tf, ok := t.filesByPart[partNumGlobal]
		if !ok {
			return len(b), nil
		}

		canWrite := int64(telegramPartSize) - offInPart
		if canWrite > int64(len(remaining)) {
			canWrite = int64(len(remaining))
		}
		
		if currentOff + canWrite > tf.offset + tf.length {
			canWrite = (tf.offset + tf.length) - currentOff
		}

		if canWrite <= 0 {
			remaining = remaining[1:]
			currentOff += 1
			continue
		}

		partNumInFile := int((currentOff - tf.offset) / telegramPartSize)
		
		if offInPart == 0 && canWrite == telegramPartSize {
			go t.uploadPartForFile(tf, partNumInFile, remaining[:canWrite])
		} else if currentOff + canWrite == tf.offset + tf.length {
			go t.uploadPartForFile(tf, partNumInFile, remaining[:canWrite])
		}

		remaining = remaining[canWrite:]
		currentOff += canWrite
	}
	
	return len(b), nil
}

func (p *telegramPiece) MarkComplete() error {
	t := p.t
	torrent := p.piece.Torrent()
	
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, tf := range t.files {
		if tf.committed {
			continue
		}
		
		for _, af := range torrent.Files() {
			if af.DisplayPath() == tf.name {
				if af.BytesCompleted() == af.Length() {
					go t.commitFile(tf)
				}
				break
			}
		}
	}

	return nil
}

func (p *telegramPiece) MarkNotComplete() error {
	return nil
}

func (p *telegramPiece) Completion() (storage.Completion, error) {
	// We should probably check if we have the data, but for now just say we don't 
	// to force redownload if needed.
	return storage.Completion{
		Complete: false,
		Ok:       true,
	}, nil
}
