package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// Client wraps the gotd/td Telegram client and handles authentication state.
type Client struct {
	apiID      int
	apiHash    string
	sessionDir string

	client *telegram.Client
	raw    *tg.Client

	mu            sync.Mutex
	phone         string
	phoneCodeHash string
	
	// runner fields
	cancel context.CancelFunc
	running bool
}

// NewClient initializes a new Telegram client wrapper.
func NewClient(apiID int, apiHash string, sessionDir string) *Client {
	return &Client{
		apiID:      apiID,
		apiHash:    apiHash,
		sessionDir: sessionDir,
	}
}

// EnsureConnected starts the Telegram client if it's not already running.
func (c *Client) EnsureConnected(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	if err := os.MkdirAll(c.sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	sessionFile := filepath.Join(c.sessionDir, "session.json")
	
	c.client = telegram.NewClient(c.apiID, c.apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{
			Path: sessionFile,
		},
	})
	c.raw = tg.NewClient(c.client)

	runCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	
	ready := make(chan struct{})
	errChan := make(chan error, 1)

	go func() {
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()

		defer func() {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
		}()
		
		err := c.client.Run(runCtx, func(ctx context.Context) error {
			close(ready)
			<-ctx.Done()
			return ctx.Err()
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Printf("Telegram client runner error: %v\n", err)
			select {
			case errChan <- err:
			default:
			}
		}
	}()

	// Wait for client to be ready or fail
	select {
	case <-ready:
		return nil
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Raw returns the underlying tg.Client for direct API calls.
func (c *Client) Raw() *tg.Client {
	return c.raw
}

// Stop shuts down the Telegram client.
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

// IsAuthenticated checks if the current session is authorized.
func (c *Client) IsAuthenticated(ctx context.Context) (bool, error) {
	if !c.running {
		return false, nil
	}
	
	status, err := c.client.Auth().Status(ctx)
	if err != nil {
		return false, err
	}
	return status.Authorized, nil
}

// RequestCode sends a login code to the provided phone number.
func (c *Client) RequestCode(ctx context.Context, phone string) (string, error) {
	c.mu.Lock()
	c.phone = phone
	c.mu.Unlock()

	sentCode, err := c.client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
	if err != nil {
		return "", err
	}

	sc, ok := sentCode.(*tg.AuthSentCode)
	if !ok {
		return "", fmt.Errorf("unexpected sent code type: %T", sentCode)
	}

	c.mu.Lock()
	c.phoneCodeHash = sc.PhoneCodeHash
	c.mu.Unlock()

	return "code_sent", nil
}

// SignIn completes the authentication with the received code.
func (c *Client) SignIn(ctx context.Context, code string) (bool, string, error) {
	c.mu.Lock()
	phone := c.phone
	phoneCodeHash := c.phoneCodeHash
	c.mu.Unlock()

	if phone == "" || phoneCodeHash == "" {
		return false, "", errors.New("no active login session; request code first")
	}

	_, err := c.client.Auth().SignIn(ctx, phone, code, phoneCodeHash)
	if err != nil {
		// Check for 2FA requirement
		if errors.Is(err, auth.ErrPasswordAuthNeeded) {
			return false, "password_required", nil
		}
		return false, "", err
	}

	return true, "success", nil
}

// CheckPassword completes the authentication with 2FA password.
func (c *Client) CheckPassword(ctx context.Context, password string) (bool, error) {
	_, err := c.client.Auth().Password(ctx, password)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Logout signs out the user and clears the session.
func (c *Client) Logout(ctx context.Context) error {
	if _, err := c.raw.AuthLogOut(ctx); err != nil {
		return err
	}
	
	c.Stop()
	
	sessionFile := filepath.Join(c.sessionDir, "session.json")
	return os.Remove(sessionFile)
}
