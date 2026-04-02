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
	tokenDir      = ".kin-code"
	tokenFileName = "oauth.json"
)

// OAuthTokens holds the OAuth access and refresh tokens.
type OAuthTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (t *OAuthTokens) isExpired() bool {
	return time.Now().Add(5 * time.Minute).After(t.ExpiresAt)
}

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

// OAuthLogin performs the full OAuth PKCE flow.
func OAuthLogin() (*OAuthTokens, error) {
	// Generate PKCE code_verifier (64 chars) and code_challenge.
	verifier, err := randomString(64)
	if err != nil {
		return nil, fmt.Errorf("generating code verifier: %w", err)
	}
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	// Generate state for CSRF protection.
	state, err := randomString(32)
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}

	// Find an available port for the callback server.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	// Build authorization URL.
	params := url.Values{
		"code":                  {"true"},
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {"user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"},
		"state":                 {state},
	}
	authURL := authEndpoint + "?" + params.Encode()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if errMsg := q.Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("authorization error: %s — %s", errMsg, q.Get("error_description"))
			fmt.Fprintf(w, "<html><body><h2>Authorization failed</h2><p>%s</p><p>You can close this tab.</p></body></html>", errMsg)
			return
		}

		code := q.Get("code")
		returnedState := q.Get("state")

		if code == "" {
			// Code might be in a fragment — serve JS to extract it.
			fmt.Fprintf(w, `<html><body><script>
				var hash = window.location.hash.substring(1);
				if (hash) {
					window.location.href = "/callback?code=" + encodeURIComponent(hash);
				} else {
					document.body.innerHTML = '<h2>Waiting for authorization...</h2>';
				}
			</script></body></html>`)
			return
		}

		// Anthropic may return "code#state" format.
		if strings.Contains(code, "#") {
			parts := strings.SplitN(code, "#", 2)
			code = parts[0]
			if returnedState == "" && len(parts) > 1 {
				returnedState = parts[1]
			}
		}

		if returnedState != "" && returnedState != state {
			errCh <- fmt.Errorf("state mismatch")
			fmt.Fprint(w, "<html><body><h2>State mismatch</h2><p>You can close this tab.</p></body></html>")
			return
		}

		codeCh <- code
		fmt.Fprint(w, `<html><body style="font-family:system-ui;text-align:center;padding:60px">
<h2>Login successful!</h2>
<p>You can close this tab and return to kin-code.</p>
</body></html>`)
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	fmt.Println("Opening browser for Claude login...")
	fmt.Printf("If the browser doesn't open, visit:\n%s\n\n", authURL)
	openBrowser(authURL)
	fmt.Println("Waiting for authorization (timeout: 5 minutes)...")

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("login timed out after 5 minutes")
	}

	fmt.Println("Exchanging authorization code for tokens...")
	tokens, err := exchangeCode(code, verifier, redirectURI, state)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}

	if err := saveTokens(tokens); err != nil {
		return nil, fmt.Errorf("saving tokens: %w", err)
	}

	fmt.Println("Login successful! Tokens saved to ~/.kin-code/oauth.json")
	return tokens, nil
}

// LoadTokens reads saved OAuth tokens.
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

// RefreshTokens uses refresh_token to get new tokens.
func RefreshTokens(refreshToken string) (*OAuthTokens, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}
	resp, err := postOAuthForm(tokenEndpoint, form)
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

// GetValidToken returns a valid OAuth access token, refreshing if needed.
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

// GetValidAPIKey exchanges OAuth token for a temporary API key.
// Claude 4.x models require a proper API key; raw OAuth tokens don't work.
func GetValidAPIKey() (string, error) {
	oauthToken, err := GetValidToken()
	if err != nil {
		return "", err
	}
	return createAPIKey(oauthToken)
}

func createAPIKey(oauthToken string) (string, error) {
	req, err := http.NewRequest("POST",
		"https://api.anthropic.com/api/oauth/claude_cli/create_api_key",
		strings.NewReader("{}"))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+oauthToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-code/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("api key exchange: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api key exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		RawKey string `json:"raw_key"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing api key: %w", err)
	}
	if result.RawKey == "" {
		return "", fmt.Errorf("no api_key in response: %s", string(body))
	}
	return result.RawKey, nil
}

// --- internal helpers ---

func postOAuthForm(endpoint string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "claude-code/1.0")
	req.Header.Set("Accept", "application/json")
	return http.DefaultClient.Do(req)
}

func exchangeCode(code, verifier, redirectURI, state string) (*OAuthTokens, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
		"state":         {state},
	}
	resp, err := postOAuthForm(tokenEndpoint, form)
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
