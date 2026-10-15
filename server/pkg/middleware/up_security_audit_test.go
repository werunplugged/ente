package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/utils/auth"
	"github.com/gin-gonic/gin"
)

func captureUPAudit(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	upAuditLogger.SetOutput(buf)
	t.Cleanup(func() { upAuditLogger.SetOutput(os.Stdout) })
	upAuditLinkAccessSeen.Flush()
	upAuditUPTokenSeen.Flush()
	return buf
}

func upAuditEvents(t *testing.T, buf *bytes.Buffer) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var doc map[string]interface{}
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("not JSON: %q: %v", line, err)
		}
		out = append(out, doc)
	}
	return out
}

// field reads a dotted path from a nested event.
func field(doc map[string]interface{}, path string) interface{} {
	var cur interface{} = doc
	for _, p := range strings.Split(path, ".") {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func expectFields(t *testing.T, e map[string]interface{}, want map[string]interface{}) {
	t.Helper()
	for path, v := range want {
		if got := field(e, path); got != v {
			t.Errorf("%s = %v, want %v", path, got, v)
		}
	}
}

// fakeTokenAuth mimics the auth middleware: it sets the identity headers.
func fakeTokenAuth(c *gin.Context) {
	if c.GetHeader("X-Auth-Token") != "valid" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	c.Request.Header.Set(AuthUserID, "7")
}

func newAuditRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(UPSecurityAudit())

	r.POST("/users/verify-email", func(c *gin.Context) {
		var req map[string]string
		_ = c.ShouldBindJSON(&req)
		switch req["ott"] {
		case "good":
			c.JSON(http.StatusOK, gin.H{"id": 42, "encryptedToken": "secret-token-value"})
		case "mfa":
			c.JSON(http.StatusOK, gin.H{"id": 42, "twoFactorSessionID": "s"})
		default:
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "INCORRECT_OTT", "message": "incorrect ott"})
		}
	})

	private := r.Group("/", fakeTokenAuth)
	private.POST("/files", func(c *gin.Context) {
		var f map[string]interface{}
		if err := c.ShouldBindJSON(&f); err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": 9007199254740993, "collectionID": f["collectionID"]})
	})
	private.POST("/files/trash", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		if len(body) == 0 {
			c.AbortWithStatus(http.StatusBadRequest) // body must still reach the handler
			return
		}
		c.Status(http.StatusOK)
	})
	private.DELETE("/files/share-url/:fileID", func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "PERMISSION_DENIED"})
	})
	private.GET("/files/download/:fileID", func(c *gin.Context) { c.Status(http.StatusForbidden) })
	private.GET("/collections/v2", func(c *gin.Context) { c.Status(http.StatusOK) })

	up := r.Group("/", func(c *gin.Context) {
		c.Request.Header.Set(UpUsernameHeader, "alice")
		c.Request.Header.Set(AuthUserID, "11")
	})
	up.POST("/users/up/ott", func(c *gin.Context) { c.Status(http.StatusOK) })

	link := r.Group("/public-collection", func(c *gin.Context) {
		switch c.GetHeader("X-Auth-Access-Token") {
		case "good":
			c.Set(auth.PublicAccessKey, ente.PublicAccessContext{ID: 3, CollectionID: 99})
		case "expired":
			c.AbortWithStatusJSON(http.StatusGone, gin.H{"error": "expired token"})
		default:
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		}
	})
	link.GET("/files/preview/:fileID", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func doRequest(r *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("User-Agent", "test-agent")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUPAuditLogin(t *testing.T) {
	buf := captureUPAudit(t)
	r := newAuditRouter()
	doRequest(r, http.MethodPost, "/users/verify-email", `{"email":"bob","ott":"good"}`, nil)
	doRequest(r, http.MethodPost, "/users/verify-email", `{"email":"bob","ott":"mfa"}`, nil)
	doRequest(r, http.MethodPost, "/users/verify-email", `{"email":"bob","ott":"bad"}`, nil)

	evs := upAuditEvents(t, buf)
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d: %s", len(evs), buf.String())
	}
	expectFields(t, evs[0], map[string]interface{}{
		"labels.log_type":     "security",
		"log.logger":          upAuditLoggerName,
		"event.action":        "user-login",
		"event.category":      "authentication",
		"event.type":          "start",
		"event.outcome":       "success",
		"user.id":             "42",
		"user.name":           "bob",
		"labels.auth_method":  "email-ott",
		"user_agent.original": "test-agent",
		"url.path":            "/users/verify-email",
	})
	ts, _ := field(evs[0], "@timestamp").(string)
	if len(ts) != len(upAuditTimestampLayout) || !strings.HasSuffix(ts, "Z") {
		t.Errorf("@timestamp %q is not UTC millisecond ISO-8601", ts)
	}
	expectFields(t, evs[1], map[string]interface{}{"event.action": "user-login-first-factor", "event.outcome": "success"})
	expectFields(t, evs[2], map[string]interface{}{
		"event.action":              "user-login",
		"event.outcome":             "failure",
		"user.name":                 "bob",
		"event.reason":              "INCORRECT_OTT",
		"http.response.status_code": float64(401),
	})
	if strings.Contains(buf.String(), "secret-token-value") || strings.Contains(buf.String(), `"good"`) {
		t.Error("a token or OTT leaked into the security log")
	}
}

func TestUPAuditFiles(t *testing.T) {
	buf := captureUPAudit(t)
	r := newAuditRouter()
	auth := map[string]string{"X-Auth-Token": "valid"}
	doRequest(r, http.MethodPost, "/files", `{"id":0,"collectionID":5}`, auth)
	doRequest(r, http.MethodPost, "/files", `{"id":12,"collectionID":5}`, auth)
	w := doRequest(r, http.MethodPost, "/files/trash", `{"items":[{"fileID":1,"collectionID":5},{"fileID":2,"collectionID":5}]}`, auth)
	if w.Code != http.StatusOK {
		t.Fatalf("request body did not reach the handler: status %d", w.Code)
	}

	evs := upAuditEvents(t, buf)
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d: %s", len(evs), buf.String())
	}
	expectFields(t, evs[0], map[string]interface{}{
		"event.action":         "file-uploaded",
		"event.type":           "creation",
		"user.id":              "7",
		"labels.resource_type": "file",
		"labels.resource_id":   "9007199254740993", // int64 precision kept
		"labels.collection_id": "5",
	})
	expectFields(t, evs[1], map[string]interface{}{"event.action": "file-updated", "labels.resource_id": "12"})
	expectFields(t, evs[2], map[string]interface{}{
		"event.action":          "file-trashed",
		"labels.resource_id":    "1,2",
		"labels.resource_count": "2",
	})
}

func TestUPAuditAuthFailures(t *testing.T) {
	buf := captureUPAudit(t)
	r := newAuditRouter()
	doRequest(r, http.MethodGet, "/collections/v2?token=leaky", "", nil)
	doRequest(r, http.MethodGet, "/files/download/5", "", map[string]string{"X-Auth-Token": "valid"})
	doRequest(r, http.MethodDelete, "/files/share-url/8", "", map[string]string{"X-Auth-Token": "valid"})
	doRequest(r, http.MethodGet, "/collections/v2", "", map[string]string{"X-Auth-Token": "valid"}) // not logged

	evs := upAuditEvents(t, buf)
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d: %s", len(evs), buf.String())
	}
	expectFields(t, evs[0], map[string]interface{}{"event.action": "credential-validated", "event.outcome": "failure", "event.reason": "invalid token"})
	expectFields(t, evs[1], map[string]interface{}{"event.action": "access-denied", "user.id": "7", "url.path": "/files/download/:fileID"})
	expectFields(t, evs[2], map[string]interface{}{
		"event.action":       "file-link-disabled",
		"event.outcome":      "failure",
		"labels.resource_id": "8",
		"event.reason":       "PERMISSION_DENIED",
	})
	if strings.Contains(buf.String(), "leaky") {
		t.Error("query string leaked into the security log")
	}
}

func TestUPAuditSpoofedIdentityIgnored(t *testing.T) {
	buf := captureUPAudit(t)
	r := newAuditRouter()
	doRequest(r, http.MethodPost, "/users/verify-email", `{"email":"bob","ott":"bad"}`,
		map[string]string{AuthUserID: "1", UpUsernameHeader: "admin"})
	e := upAuditEvents(t, buf)[0]
	expectFields(t, e, map[string]interface{}{"user.name": "bob", "labels.identity_untrusted": "true"})
	if field(e, "user.id") != nil {
		t.Errorf("client-sent user id was trusted: %v", field(e, "user.id"))
	}
}

func TestUPAuditUPTokenFirstSeen(t *testing.T) {
	buf := captureUPAudit(t)
	r := newAuditRouter()
	h := map[string]string{"Authorization": "Bearer abc"}
	doRequest(r, http.MethodPost, "/users/up/ott", "", h)
	doRequest(r, http.MethodPost, "/users/up/ott", "", h)

	evs := upAuditEvents(t, buf)
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d: %s", len(evs), buf.String())
	}
	expectFields(t, evs[0], map[string]interface{}{
		"event.action":       "credential-validated",
		"event.outcome":      "success",
		"user.name":          "alice",
		"user.id":            "11",
		"labels.auth_method": "up-jwt",
	})
	if strings.Contains(buf.String(), "abc") {
		t.Error("access token leaked into the security log")
	}
}

func TestUPAuditLinkAccess(t *testing.T) {
	buf := captureUPAudit(t)
	r := newAuditRouter()
	for _, token := range []string{"good", "good", "expired", "bad"} {
		doRequest(r, http.MethodGet, "/public-collection/files/preview/1", "", map[string]string{"X-Auth-Access-Token": token})
	}
	evs := upAuditEvents(t, buf)
	if len(evs) != 3 {
		t.Fatalf("want 3 events (allowed once, 2 denied), got %d: %s", len(evs), buf.String())
	}
	expectFields(t, evs[0], map[string]interface{}{
		"event.action": "shared-resource-accessed", "event.type": "allowed", "labels.resource_id": "99", "labels.link_id": "3",
	})
	expectFields(t, evs[1], map[string]interface{}{"event.type": "denied", "event.reason": "expired token", "labels.resource_type": "collection_link"})
	expectFields(t, evs[2], map[string]interface{}{"event.reason": "invalid token", "http.response.status_code": float64(401)})
}

func TestUPAuditSanitize(t *testing.T) {
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl"
	for _, in := range []string{"Authorization: Bearer abc.def-ghi", "token=s3cr3tvalue&x=1", `{"password": "hunter2"}`, "ott=123456", "jwt " + jwt} {
		out := upAuditSanitize(in)
		for _, secret := range []string{"abc.def-ghi", "s3cr3tvalue", "hunter2", "123456", jwt} {
			if strings.Contains(out, secret) {
				t.Errorf("upAuditSanitize(%q) = %q still contains %q", in, out, secret)
			}
		}
	}
	if got := upAuditSanitize(strings.Repeat("a", 2000)); len(got) > upAuditMaxValueLength+3 {
		t.Errorf("not truncated: len %d", len(got))
	}
}
