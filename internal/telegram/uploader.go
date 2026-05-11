package telegram

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// UploadFile uploads a local file to the user's "Saved Messages" (InputPeerSelf).
func (c *Client) UploadFile(ctx context.Context, filePath string, fileName string) error {
	raw := c.Raw()
	if raw == nil {
		return errors.New("telegram client not connected")
	}

	u := uploader.NewUploader(raw)
	
	// Create an upload from the file path
	f, err := u.FromPath(ctx, filePath)
	if err != nil {
		return fmt.Errorf("failed to prepare upload from path: %w", err)
	}

	// Finalize the upload by sending it as a document
	var randomID int64
	if err := binary.Read(rand.Reader, binary.LittleEndian, &randomID); err != nil {
		return fmt.Errorf("failed to generate random ID: %w", err)
	}

	_, err = raw.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Peer: &tg.InputPeerSelf{},
		Media: &tg.InputMediaUploadedDocument{
			File:     f,
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
