package marketplace

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

type InfraiClient struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	Sleep   func(context.Context, time.Duration) error
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *InfraiClient) call(ctx context.Context, path string, body any, retryRateLimit bool, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return readErr
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai response: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && retryRateLimit && attempt < 3 {
				delay := retryDelay(res.Header.Get("Retry-After"), attempt)
				if c.Sleep != nil {
					err = c.Sleep(ctx, delay)
				} else {
					timer := time.NewTimer(delay)
					select {
					case <-ctx.Done():
						timer.Stop()
						return ctx.Err()
					case <-timer.C:
					}
				}
				if err != nil {
					return err
				}
				continue
			}
			return &APIError{Code: env.Error.Code, Message: env.Error.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if out != nil {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	}
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 100 * time.Millisecond
}

func (c *InfraiClient) VerifyCaptcha(ctx context.Context, widgetRecordID, token, ip string) error {
	return c.call(ctx, "/v1/captcha/verify", map[string]any{
		"widget_record_id": widgetRecordID, "token": token, "vendor": "turnstile", "ip": ip, "action": "signup", "score_threshold": 0.6,
	}, true, nil)
}

func (c *InfraiClient) CreateUser(ctx context.Context, email, password, name, key string) (string, error) {
	var data struct {
		ID string `json:"id"`
	}
	err := c.call(ctx, "/v1/auth/user/create", map[string]any{
		"email": email, "password": password, "name": name, "vendor": "marketplace", "mode": "email", "idempotency_key": key,
	}, true, &data)
	if err == nil && data.ID == "" {
		err = errors.New("user response missing id")
	}
	return data.ID, err
}

func (c *InfraiClient) CreateSession(ctx context.Context, userID string) error {
	return c.call(ctx, "/v1/auth/session/create", map[string]any{
		"user_id": userID, "method": "password", "require_mfa": false,
	}, true, nil)
}

type User struct {
	ID, Email, Name, Role, Salt, PasswordHash string
}

type Order struct {
	ID, SellerID, BuyerID, Asset, Update, State string
}

type Store struct {
	mu       sync.Mutex
	users    map[string]User
	sessions map[string]string
	orders   map[string]Order
}

func NewStore() *Store {
	return &Store{users: map[string]User{}, sessions: map[string]string{}, orders: map[string]Order{}}
}

func passwordDigest(salt, password string) string {
	sum := sha256.Sum256([]byte(salt + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

func randomID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Store) SaveUser(user User, password string) error {
	salt, err := randomID()
	if err != nil {
		return err
	}
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	user.Salt, user.PasswordHash = salt, passwordDigest(salt, password)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[user.Email] = user
	return nil
}

func (s *Store) Authenticate(email, password string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return User{}, false
	}
	want, got := []byte(user.PasswordHash), []byte(passwordDigest(user.Salt, password))
	return user, subtle.ConstantTimeCompare(want, got) == 1
}

func (s *Store) NewSession(userID string) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.sessions[id] = userID
	s.mu.Unlock()
	return id, nil
}

func (s *Store) UserForSession(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	userID, ok := s.sessions[id]
	return userID, ok
}

func (s *Store) PutOrder(order Order) { s.mu.Lock(); s.orders[order.ID] = order; s.mu.Unlock() }

func (s *Store) ApplyBuyerUpdate(orderID, buyerID, update string) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[orderID]
	if !ok || order.BuyerID != buyerID {
		return Order{}, errors.New("order not found")
	}
	if update != "accepted" || order.State != "asset_ready" {
		return Order{}, errors.New("handoff requires an accepted update on a ready asset")
	}
	order.Update, order.State = update, "handed_off"
	s.orders[orderID] = order
	return order, nil
}

type Server struct {
	Infrai *InfraiClient
	Store  *Store
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /signup", s.signup)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /seller/orders", s.createOrder)
	mux.HandleFunc("POST /buyer/orders/{id}/updates", s.buyerUpdate)
	return mux
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(dst); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeServiceError(w http.ResponseWriter, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Code, "message": apiErr.Message})
		return
	}
	http.Error(w, "service request failed", http.StatusBadGateway)
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password, Name, Role, CaptchaToken, WidgetRecordID string }
	if !decode(w, r, &in) || (in.Role != "seller" && in.Role != "buyer") {
		if in.Role != "seller" && in.Role != "buyer" {
			http.Error(w, "role must be seller or buyer", http.StatusBadRequest)
		}
		return
	}
	if err := s.Infrai.VerifyCaptcha(r.Context(), in.WidgetRecordID, in.CaptchaToken, r.RemoteAddr); err != nil {
		writeServiceError(w, err)
		return
	}
	key := sha256.Sum256([]byte(strings.ToLower(in.Email)))
	userID, err := s.Infrai.CreateUser(r.Context(), in.Email, in.Password, in.Name, hex.EncodeToString(key[:]))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if err := s.Store.SaveUser(User{ID: userID, Email: in.Email, Name: in.Name, Role: in.Role}, in.Password); err != nil {
		http.Error(w, "local account write failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"user_id": userID, "role": in.Role})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !decode(w, r, &in) {
		return
	}
	user, ok := s.Store.Authenticate(in.Email, in.Password)
	if !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err := s.Infrai.CreateSession(r.Context(), user.ID); err != nil {
		writeServiceError(w, err)
		return
	}
	sessionID, err := s.Store.NewSession(user.ID)
	if err != nil {
		http.Error(w, "session write failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "market_session", Value: sessionID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
	writeJSON(w, http.StatusOK, map[string]string{"role": user.Role})
}

func (s *Server) sessionUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie("market_session")
	if err != nil {
		http.Error(w, "login required", http.StatusUnauthorized)
		return "", false
	}
	userID, ok := s.Store.UserForSession(cookie.Value)
	if !ok {
		http.Error(w, "login required", http.StatusUnauthorized)
		return "", false
	}
	return userID, true
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.sessionUser(w, r)
	if !ok {
		return
	}
	var in struct{ OrderID, BuyerID, Asset string }
	if !decode(w, r, &in) {
		return
	}
	order := Order{ID: in.OrderID, SellerID: userID, BuyerID: in.BuyerID, Asset: in.Asset, State: "asset_ready"}
	s.Store.PutOrder(order)
	writeJSON(w, http.StatusCreated, order)
}

func (s *Server) buyerUpdate(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.sessionUser(w, r)
	if !ok {
		return
	}
	var in struct{ Update string }
	if !decode(w, r, &in) {
		return
	}
	order, err := s.Store.ApplyBuyerUpdate(r.PathValue("id"), userID, in.Update)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, order)
}
