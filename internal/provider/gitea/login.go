package gitea

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/token"
)

// loginConfig carries the seams the loopback flow needs. Production wires the
// real HTTP client and keychain store; a test injects a mock transport and an
// in-memory store to drive the whole flow without a network or real keychain.
type loginConfig struct {
	host     string
	clientID string
	prompt   func(authURL string)
	client   *http.Client
	store    tokenStore
}

// Login authenticates clover with a Gitea/Forgejo host via the OAuth
// authorization-code flow with PKCE (RFC 8252): it binds a loopback listener,
// opens the browser to the host's authorize page through prompt, captures the
// redirected code, exchanges it for tokens, and persists them under the host so
// the credential chain finds them. host and clientID default to codeberg.org and
// Gitea's built-in public "tea" application. Gitea has no device flow, so this
// browser flow is the interactive login; it needs a local browser (not headless),
// which is fine for a one-time login command.
func Login(ctx context.Context, host, clientID string, prompt func(authURL string)) error {
	store, err := token.New()
	if err != nil {
		return err
	}
	return login(ctx, loginConfig{
		host:     host,
		clientID: clientID,
		prompt:   prompt,
		client:   &http.Client{},
		store:    store,
	})
}

// login drives the loopback flow against the injected client and store.
func login(ctx context.Context, cfg loginConfig) error {
	host, ok := forge.NormalizeHost(cmp.Or(cfg.host, defaultHost))
	if !ok {
		return fmt.Errorf("gitea: invalid host")
	}
	clientID := cmp.Or(cfg.clientID, teaClientID)

	verifier, challenge, err := forge.PKCE()
	if err != nil {
		return fmt.Errorf("gitea: generate PKCE: %w", err)
	}
	state, err := forge.RandomState()
	if err != nil {
		return fmt.Errorf("gitea: generate state: %w", err)
	}

	// A loopback listener on an ephemeral port. Gitea strips the port when matching
	// a public client's redirect against the registered http://127.0.0.1, so any
	// port is accepted.
	config := &net.ListenConfig{}
	listener, err := config.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("gitea: open loopback listener: %w", err)
	}
	defer listener.Close()
	redirectURI := fmt.Sprintf("http://%s/", listener.Addr().String())

	query := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {oauthScope},
	}
	authURL := authorizeURL(host) + "?" + query.Encode()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server := &http.Server{
		Handler:           callbackHandler(state, codeCh, errCh),
		ReadHeaderTimeout: 10 * time.Second, //nolint:mnd // self-explanatory
	}
	go func() {
		if serveErr := server.Serve(
			listener,
		); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()
	defer func() { _ = server.Shutdown(context.WithoutCancel(ctx)) }()

	cfg.prompt(authURL)

	var code string
	select {
	case code = <-codeCh:
	case cbErr := <-errCh:
		return fmt.Errorf("gitea: authorization callback: %w", cbErr)
	case <-ctx.Done():
		return ctx.Err()
	}

	c, err := exchangeCode(ctx, cfg.client, host, clientID, code, redirectURI, verifier)
	if err != nil {
		return err
	}

	//nolint:gosec // persisting the minted credential is the point
	blob, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("gitea: encode credentials: %w", err)
	}
	if err := cfg.store.Set(host, string(blob)); err != nil {
		return fmt.Errorf("gitea: store token: %w", err)
	}
	return nil
}

// callbackHandler serves the loopback redirect: it checks the state, captures the
// authorization code, and shows a page telling the user to return to the
// terminal. A mismatched state or an error response is reported on errCh.
func callbackHandler(state string, codeCh chan<- string, errCh chan<- error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			http.Error(w, "Authorization failed: "+e, http.StatusBadRequest)
			errCh <- fmt.Errorf("authorization denied: %s", e)
			return
		}
		if q.Get("state") != state {
			http.Error(w, "State mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("state mismatch")
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "No authorization code", http.StatusBadRequest)
			errCh <- fmt.Errorf("no authorization code in callback")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<!doctype html><title>Clover</title>"+
			"<body style=\"font-family:system-ui;text-align:center;padding-top:3rem\">"+
			"<h1>🍀 Clover</h1><p>Authorized. You can close this tab and return to the terminal.</p>")
		codeCh <- code
	})
}
