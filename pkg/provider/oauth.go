package provider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	authEndpoint  = "https://claude.ai/oauth/authorize"
	tokenEndpoint = "https://platform.claude.com/v1/oauth/token"
	clientID      = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	redirectPort  = "9876"
	redirectURI   = "http://localhost:" + redirectPort + "/oauth/callback"
	tokenDir      = ".kin-code"
	tokenFileName = "oauth.json"
)

// OAuthTokens holds the OAuth access and refresh tokens.
type OAuthTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// isExpired returns true if the access token has expired or will within 5 minutes.
func (t *OAuthTokens) isExpired() bool {
	return time.Now().Add(5 * time.Minute).After(t.ExpiresAt)
}

// tokenFilePath returns ~/.kin-code/oauth.json, creating the directory if needed.
func tokenFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, tokenDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, tokenFileName), nil
}

// OAuthLogin performs the full OAuth PKCE flow: opens browser, waits for
// callback, exchanges code for tokens, saves them, and returns the tokens.
func OAuthLogin() (*OAuthTokens, error) {
	// Generate PKCE code_verifier and code_challenge.
	verifier, err := randomString(43)
	if err != nil {
		return nil, fmt.Errorf("generating code verifier: %w", err)
	}
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	// Build authorization URL.
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {"user:inference"},
	}
	authURL := authEndpoint + "?" + params.Encode()

	// Channels for receiving the auth code or error from the callback.
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	// Start local HTTP server for the OAuth callback.
	listener, err := net.Listen("tcp", "127.0.0.1:"+redirectPort)
	if err != nil {
		return nil, fmt.Errorf("starting callback server on port %s: %w", redirectPort, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errMsg := q.Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("authorization error: %s — %s", errMsg, q.Get("error_description"))
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<html><body><h2>Authorization failed</h2><p>%s</p><p>You can close this tab.</p></body></html>", errMsg)
			return
		}

		code := q.Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code in callback")
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body><h2>Error: no code received</h2><p>You can close this tab.</p></body></html>")
			return
		}

		codeCh <- code
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body style="font-family:system-ui;text-align:center;padding:60px">
<h2>Login successful!</h2>
<p>You can close this tab and return to kin-code.</p>
</body></html>`)
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	// Open browser.
	fmt.Println("Opening browser for Claude login...")
	fmt.Printf("If the browser doesn't open, visit:\n%s\n\n", authURL)
	openBrowser(authURL)

	fmt.Println("Waiting for authorization (timeout: 2 minutes)...")

	// Wait for code or timeout.
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(120 * time.Second):
		return nil, fmt.Errorf("login timed out after 2 minutes")
	}

	// Exchange authorization code for tokens.
	fmt.Println("Exchanging authorization code for tokens...")
	tokens, err := exchangeCode(code, verifier)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}

	// Save tokens.
	if err := saveTokens(tokens); err != nil {
		return nil, fmt.Errorf("saving tokens: %w", err)
	}

	fmt.Println("Login successful! Tokens saved to ~/.kin-code/oauth.json")
	return tokens, nil
}

// LoadTokens reads saved OAuth tokens from ~/.kin-code/oauth.json.
func LoadTokens() (*OAuthTokens, error) {
	path, err := tokenFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tokens OAuthTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}

// RefreshTokens uses the refresh token to obtain new tokens and saves them.
func RefreshTokens(refreshToken string) (*OAuthTokens, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}

	resp, err := http.PostForm(tokenEndpoint, form)
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	tokens, err := parseTokenResponse(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := saveTokens(tokens); err != nil {
		return nil, fmt.Errorf("saving refreshed tokens: %w", err)
	}
	return tokens, nil
}

// GetValidToken returns a valid access token, refreshing if needed.
func GetValidToken() (string, error) {
	tokens, err := LoadTokens()
	if err != nil {
		return "", fmt.Errorf("no saved tokens (run: kin-code -login): %w", err)
	}

	if !tokens.isExpired() {
		return tokens.AccessToken, nil
	}

	tokens, err = RefreshTokens(tokens.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("token refresh failed (run: kin-code -login): %w", err)
	}
	return tokens.AccessToken, nil
}

// --- internal helpers ---

func exchangeCode(code, verifier string) (*OAuthTokens, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}

	resp, err := http.PostForm(tokenEndpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	return parseTokenResponse(resp.Body)
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func parseTokenResponse(body io.Reader) (*OAuthTokens, error) {
	var tr oauthTokenResponse
	if err := json.NewDecoder(body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("no access_token in response")
	}
	return &OAuthTokens{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

func saveTokens(tokens *OAuthTokens) error {
	path, err := tokenFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func randomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(b)
	if len(s) < length {
		return "", fmt.Errorf("generated string too short")
	}
	return s[:length], nil
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", strings.ReplaceAll(u, "&", "^&"))
	}
	if cmd != nil {
		cmd.Start()
	}
}
