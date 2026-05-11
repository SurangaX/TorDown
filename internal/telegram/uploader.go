package telegram

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/gotd/td/tg"
)

const telegramPartSize = 512 * 1024 // 512KB

// UploadFile uploads a local file to the user's "Saved Messages" (InputPeerSelf).
func (c *Client) UploadFile(ctx context.Context, filePath string, fileName string) error {
	raw := c.Raw()
	if raw == nil {
		return errors.New("telegram client not connected")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for upload: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	fileSize := info.Size()

	var fileID int64
	if err := binary.Read(rand.Reader, binary.LittleEndian, &fileID); err != nil {
		return fmt.Errorf("failed to generate file ID: %w", err)
	}

	totalParts := int((fileSize + telegramPartSize - 1) / telegramPartSize)
	if totalParts == 0 {
		totalParts = 1
	}

	buffer := make([]byte, telegramPartSize)
	for i := 0; i < totalParts; i++ {
		n, err := io.ReadFull(file, buffer)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return fmt.Errorf("failed to read file part %d: %w", i, err)
		}

		data := buffer[:n]
		_, err = raw.UploadSaveBigFilePart(ctx, &tg.UploadSaveBigFilePartRequest{
			FileID:         fileID,
			FilePart:       i,
			FileTotalParts: totalParts,
			Bytes:          data,
		})
		if err != nil {
			return fmt.Errorf("failed to upload part %d: %w", i, err)
		}
	}

	// Finalize the upload by sending it as a document
	var randomID int64
	if err := binary.Read(rand.Reader, binary.LittleEndian, &randomID); err != nil {
		return fmt.Errorf("failed to generate random ID: %w", err)
	}

	_, err = raw.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Peer: &tg.InputPeerSelf{},
		Media: &tg.InputMediaUploadedDocument{
			File: &tg.InputFileBig{
				ID:    fileID,
				Parts: totalParts,
				Name:  fileName,
			},
			MimeType: "application/octet-stream",
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{FileName: fileName},
			},
		},
		Message:  fmt.Sprintf("Uploaded: %s", fileName),
		RandomID: randomID,
	})
	if err != nil {
		return fmt.Errorf("failed to commit file to telegram: %w", err)
	}

	return nil
}
