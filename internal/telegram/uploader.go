package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type progressWriter struct {
	total    int64
	current  int64
	filename string
	lastPct  float64
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.current += int64(n)
	pct := float64(pw.current) / float64(pw.total) * 100
	if pct-pw.lastPct >= 5 || pct >= 100 {
		fmt.Fprintf(os.Stderr, "[Telegram] PROGRESS: \"%s\": %.1f%% (%d/%d bytes)\n", pw.filename, pct, pw.current, pw.total)
		pw.lastPct = pct
	}
	return n, nil
}

// UploadFileRequest is the JSON request structure for the Python uploader
type UploadFileRequest struct {
	APIID      int    `json:"api_id"`
	APIHash    string `json:"api_hash"`
	Phone      string `json:"phone"`
	FilePath   string `json:"file_path"`
	SessionDir string `json:"session_dir,omitempty"`
}

// UploadFileResponse is the JSON response from the Python uploader
type UploadFileResponse struct {
	Success bool   `json:"success"`
	File    string `json:"file,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// UploadFile uploads a file to Telegram Saved Messages using Python pyrogram uploader.
// For files > 2GB, automatically splits using 7zip before uploading.
func (c *Client) UploadFile(ctx context.Context, filePath string, fileName string) error {
	// Ensure file exists
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[Telegram] STARTING UPLOAD: \"%s\" (%d bytes)\n", fileName, stat.Size())

	// Get the Python uploader script path (relative to binary)
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable path: %w", err)
	}
	exeDir := filepath.Dir(exePath)
	pythonScript := filepath.Join(exeDir, "..", "python", "telegram_uploader.py")

	// Verify Python script exists
	if _, err := os.Stat(pythonScript); err != nil {
		return fmt.Errorf("python uploader script not found at %s: %w", pythonScript, err)
	}

	// Check if Python is available
	pythonCmd := "python3"
	if _, err := exec.LookPath(pythonCmd); err != nil {
		// Try 'python' on Windows
		pythonCmd = "python"
		if _, err := exec.LookPath(pythonCmd); err != nil {
			return errors.New("python3 or python not found in PATH")
		}
	}

	// Build request
	req := UploadFileRequest{
		APIID:      c.apiID,
		APIHash:    c.apiHash,
		Phone:      "", // Python will use stored session
		FilePath:   filePath,
		SessionDir: c.sessionDir,
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Execute Python uploader
	cmd := exec.CommandContext(ctx, pythonCmd, pythonScript)
	cmd.Stdin = strings.NewReader(string(reqJSON))

	// Capture stdout for response, stderr for progress
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Parse Python response from stdout
	var resp UploadFileResponse
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		return fmt.Errorf("failed to parse uploader response: %w (output: %s)", err, stdout.String())
	}

	// Log stderr (progress and debug info)
	if stderr.String() != "" {
		stderrLines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
		for _, line := range stderrLines {
			if strings.Contains(line, "[PROGRESS]") {
				// Extract progress and log
				fmt.Fprintf(os.Stderr, "[Telegram] %s\n", strings.TrimPrefix(line, "[PROGRESS] "))
			} else {
				fmt.Fprintf(os.Stderr, "%s\n", line)
			}
		}
	}

	// Check response
	if !resp.Success {
		if resp.Error != "" {
			return fmt.Errorf("upload failed: %s", resp.Error)
		}
		if resp.Message != "" {
			return fmt.Errorf("upload failed: %s", resp.Message)
		}
		return errors.New("upload failed: unknown error")
	}

	fmt.Fprintf(os.Stderr, "[Telegram] FINISHED UPLOAD: \"%s\"\n", fileName)
	return nil
}
