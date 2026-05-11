package tstorage

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
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

// OpenTorrent satisfies storage.ClientImpl
func (ts *TelegramStorage) OpenTorrent(info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if t, ok := ts.torrents[infoHash]; ok {
		t.info = info
		return t, nil
	}

	t := &telegramTorrent{
		ts:           ts,
		infoHash:     infoHash,
		info:         info,
		pieces:       make(map[int]*telegramPiece),
		filesByPart:  make(map[int64]*telegramFile),
	}
	ts.torrents[infoHash] = t
	return t, nil
}

// Close satisfies storage.ClientImpl
func (ts *TelegramStorage) Close() error {
	return nil
}

type telegramTorrent struct {
	ts       *TelegramStorage
	infoHash metainfo.Hash
	info     *metainfo.Info
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

func (t *telegramTorrent) initFiles() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.files) > 0 || t.info == nil {
		return
	}

	var currentOffset int64
	for _, f := range t.info.UpvertedFiles() {
		var id int64
		binary.Read(rand.Reader, binary.LittleEndian, &id)

		tf := &telegramFile{
			id:            id,
			name:          strings.Join(f.Path, "/"),
			offset:        currentOffset,
			length:        f.Length,
			totalParts:    int((f.Length + telegramPartSize - 1) / telegramPartSize),
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
		
		currentOffset += f.Length
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

	resp, err := raw.MessagesSendMedia(context.Background(), &tg.MessagesSendMediaRequest{
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
		if u, ok := resp.(*tg.Updates); ok {
			for _, m := range u.Updates {
				if nm, ok := m.(*tg.UpdateNewMessage); ok {
					if msg, ok := nm.Message.(*tg.Message); ok {
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

// Close satisfies storage.TorrentImpl
func (t *telegramTorrent) Close() error {
	return nil
}

// Drop satisfies storage.TorrentImpl (some versions of the library)
func (t *telegramTorrent) Drop() error {
	return nil
}

// Piece satisfies storage.TorrentImpl
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

// ReadAt satisfies storage.PieceImpl (embedded io.ReaderAt)
func (p *telegramPiece) ReadAt(b []byte, off int64) (n int, err error) {
	return 0, io.EOF
}

// WriteAt satisfies storage.PieceImpl (embedded io.WriterAt)
func (p *telegramPiece) WriteAt(b []byte, off int64) (n int, err error) {
	t := p.t
	if t.info == nil {
		return len(b), nil
	}
	t.initFiles()

	globalOff := int64(p.piece.Index())*t.info.PieceLength + off
	
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

// MarkComplete satisfies storage.PieceImpl
func (p *telegramPiece) MarkComplete() error {
	return nil
}

// MarkNotComplete satisfies storage.PieceImpl
func (p *telegramPiece) MarkNotComplete() error {
	return nil
}

// Completion satisfies storage.PieceImpl
func (p *telegramPiece) Completion() storage.Completion {
	return storage.Completion{
		Complete: false,
		Ok:       false,
	}
}
