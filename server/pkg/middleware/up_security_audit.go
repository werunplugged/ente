package middleware

// Unplugged security event logging (UNP-8993), capture side. One global
// middleware recognises the audited routes from a table and derives each
// event from the request, the response status and the JSON bodies, so no
// upstream Ente handler has to change. Output goes through
// writeUPAuditEvent (up_security_audit_log.go).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/utils/auth"
	"github.com/ente-io/museum/pkg/utils/network"
	"github.com/gin-gonic/gin"
	"github.com/patrickmn/go-cache"
)

const (
	upAuditMaxRequestBody  = 1 << 20  // larger bodies are passed through unparsed
	upAuditMaxResponseBody = 64 << 10 // for routes whose response carries the resource ID
	upAuditMaxErrorBody    = 512      // enough for {"error": "..."} or an ente.ApiError
	upAuditMaxIDsInLabel   = 100
)

const (
	upAuditCategoryAuthentication = "authentication"
	upAuditCategoryIAM            = "iam"
	upAuditCategoryFile           = "file"
	upAuditCategoryWeb            = "web"
)

var (
	// One album view fetches many thumbnails, so an allowed shared-link access
	// is logged once per link, client and user agent in this window.
	upAuditLinkAccessSeen = cache.New(10*time.Minute, 20*time.Minute)
	// The first request with a given UP access token on this instance.
	upAuditUPTokenSeen = cache.New(time.Hour, 2*time.Hour)
)

// upAuditRequest is what a route's build func can read.
type upAuditRequest struct {
	c    *gin.Context
	req  map[string]interface{} // request JSON, nil if not read or not JSON
	resp map[string]interface{} // response JSON, nil if not read or not JSON
}

type upAuditRoute struct {
	action       string
	category     string
	eventType    string
	readRequest  bool
	readResponse bool
	// build fills subject, target and resource fields
	build func(r upAuditRequest, e *upAuditEvent)
}

// upAuditRoutes is keyed by "<METHOD> <route template>" as registered in main.go.
var upAuditRoutes = map[string]upAuditRoute{
	// Logins (public routes, so the user comes from the bodies)
	"POST /users/verify-email":               upAuditLogin("email-ott", true),
	"POST /users/srp/verify-session":         upAuditLogin("srp", false),
	"POST /users/two-factor/verify":          upAuditLogin("totp", false),
	"POST /users/two-factor/passkeys/finish": upAuditLogin("passkey", false),

	// Sessions and accounts
	"POST /users/logout":    {action: "user-logout", category: upAuditCategoryAuthentication, eventType: "end"},
	"DELETE /users/session": {action: "session-terminated", category: upAuditCategoryAuthentication, eventType: "end"},
	"DELETE /users/delete": {action: "user-deleted", category: upAuditCategoryIAM, eventType: "deletion",
		build: func(r upAuditRequest, e *upAuditEvent) {
			e.TargetUserID, e.ResourceType, e.ResourceID = e.UserID, "user", upAuditFormatID(e.UserID)
		}},
	"DELETE /up-admin/user/delete": {action: "user-deleted", category: upAuditCategoryIAM, eventType: "deletion",
		build: func(r upAuditRequest, e *upAuditEvent) {
			e.TargetUserName, e.ResourceType = strings.TrimSpace(r.c.Query("username")), "user"
		}},

	// Files
	"POST /files": {category: upAuditCategoryFile, readRequest: true, readResponse: true,
		build: func(r upAuditRequest, e *upAuditEvent) {
			e.ResourceType = "file"
			upAuditLabel(e, "collection_id", upAuditFormatID(upAuditNum(r.req, "collectionID")))
			if id := upAuditNum(r.req, "id"); id != 0 {
				e.Action, e.Type, e.ResourceID = "file-updated", "change", upAuditFormatID(id)
				return
			}
			e.Action, e.Type, e.ResourceID = "file-uploaded", "creation", upAuditFormatID(upAuditNum(r.resp, "id"))
		}},
	"PUT /files/update": {action: "file-updated", category: upAuditCategoryFile, eventType: "change", readRequest: true,
		build: func(r upAuditRequest, e *upAuditEvent) {
			e.ResourceType, e.ResourceID = "file", upAuditFormatID(upAuditNum(r.req, "id"))
			upAuditLabel(e, "collection_id", upAuditFormatID(upAuditNum(r.req, "collectionID")))
		}},
	"POST /files/trash": {action: "file-trashed", category: upAuditCategoryFile, eventType: "deletion", readRequest: true,
		build: func(r upAuditRequest, e *upAuditEvent) {
			var ids []int64
			items, _ := r.req["items"].([]interface{})
			for _, item := range items {
				if m, ok := item.(map[string]interface{}); ok {
					ids = append(ids, upAuditNum(m, "fileID"))
				}
			}
			upAuditFiles(e, ids)
		}},
	"POST /trash/delete": {action: "file-deleted", category: upAuditCategoryFile, eventType: "deletion", readRequest: true,
		build: func(r upAuditRequest, e *upAuditEvent) { upAuditFiles(e, upAuditNums(r.req, "fileIDs")) }},
	"POST /trash/empty": {action: "trash-emptied", category: upAuditCategoryFile, eventType: "deletion",
		build: func(r upAuditRequest, e *upAuditEvent) { e.ResourceType = "file" }},
	"POST /files/copy": {action: "file-copied", category: upAuditCategoryFile, eventType: "creation", readRequest: true,
		build: func(r upAuditRequest, e *upAuditEvent) {
			e.ResourceType, e.ResourceID = "collection", upAuditFormatID(upAuditNum(r.req, "dstCollectionID"))
			files, _ := r.req["files"].([]interface{})
			upAuditLabel(e, "source_collection_id", upAuditFormatID(upAuditNum(r.req, "srcCollectionID")))
			upAuditLabel(e, "resource_count", strconv.Itoa(len(files)))
		}},

	// Collection sharing
	"POST /collections/share":   upAuditShare("permission-granted", "creation"),
	"POST /collections/unshare": upAuditShare("permission-revoked", "deletion"),
	"POST /collections/join-link": {action: "shared-resource-joined", category: upAuditCategoryIAM, eventType: "access", readRequest: true,
		build: upAuditCollectionFromBody},
	"DELETE /collections/v3/:collectionID": {action: "collection-deleted", category: upAuditCategoryFile, eventType: "deletion",
		build: upAuditCollectionFromPath},

	// Public links
	"POST /collections/share-url":                 upAuditLink("collection-link-created", "collectionID", true),
	"PUT /collections/share-url":                  upAuditLink("collection-link-updated", "collectionID", true),
	"DELETE /collections/share-url/:collectionID": upAuditLink("collection-link-disabled", "collectionID", false),
	"POST /files/share-url":                       upAuditLink("file-link-created", "fileID", true),
	"PUT /files/share-url":                        upAuditLink("file-link-updated", "fileID", true),
	"DELETE /files/share-url/:fileID":             upAuditLink("file-link-disabled", "fileID", false),
}

// UPSecurityAudit returns the global security audit middleware. Register it
// with server.Use after gzip so it sees uncompressed response bodies, and
// before any route group so it also sees responses written by auth middleware.
func UPSecurityAudit() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "" { // unknown route
			c.Next()
			return
		}
		// Identity headers are set by the auth middleware. A client never sends
		// them, so a value present before c.Next() is not trusted.
		sentUserID := c.Request.Header.Get(AuthUserID)
		sentUserName := c.Request.Header.Get(UpUsernameHeader)

		route, audited := upAuditRoutes[c.Request.Method+" "+path]
		var reqBody []byte
		if audited && route.readRequest {
			reqBody = upAuditPeekBody(c)
		}
		w := &upAuditWriter{ResponseWriter: c.Writer, captureAll: audited && route.readResponse}
		c.Writer = w

		c.Next()

		status := w.Status()
		e := upAuditEvent{StatusCode: status, Outcome: upAuditOutcomeOf(status)}
		if sentUserID == "" {
			e.UserID, _ = strconv.ParseInt(c.Request.Header.Get(AuthUserID), 10, 64)
		}
		if sentUserName == "" {
			e.UserName = c.Request.Header.Get(UpUsernameHeader)
		}
		if sentUserID != "" || sentUserName != "" {
			upAuditLabel(&e, "identity_untrusted", "true")
		}
		if status >= http.StatusBadRequest {
			e.Reason = upAuditErrorMessage(w.body.Bytes())
		}
		isAdmin := strings.HasPrefix(path, "/admin/") || strings.HasPrefix(path, "/up-admin/")

		switch {
		case audited:
			e.Action, e.Category, e.Type = route.action, route.category, route.eventType
			if route.build != nil {
				r := upAuditRequest{c: c, req: upAuditParseJSON(reqBody)}
				if route.readResponse {
					r.resp = upAuditParseJSON(w.body.Bytes())
				}
				route.build(r, &e)
			}
			writeUPAuditEvent(c, e)
		case strings.HasPrefix(path, "/public-collection/") || strings.HasPrefix(path, "/file-link/"):
			upAuditLinkAccess(c, e)
		case isAdmin && c.Request.Method != http.MethodGet &&
			status != http.StatusUnauthorized && status != http.StatusForbidden:
			e.Action, e.Category, e.Type = "admin-api-called", upAuditCategoryIAM, "admin"
			writeUPAuditEvent(c, e)
		case status == http.StatusUnauthorized:
			e.Action, e.Category, e.Type = "credential-validated", upAuditCategoryAuthentication, "denied"
			writeUPAuditEvent(c, e)
		case status == http.StatusForbidden:
			e.Action, e.Category, e.Type = "access-denied", upAuditCategoryWeb, "denied"
			writeUPAuditEvent(c, e)
		}

		upAuditTrackUPToken(c, e, sentUserName)
	}
}

// upAuditTrackUPToken logs the first request with a valid Unplugged access
// token on this instance. Invalid tokens are logged as 401s above.
func upAuditTrackUPToken(c *gin.Context, e upAuditEvent, sentUserName string) {
	authHeader := c.Request.Header.Get("Authorization")
	if sentUserName != "" || e.UserName == "" || authHeader == "" || e.StatusCode == http.StatusUnauthorized {
		return
	}
	if upAuditUPTokenSeen.Add(upAuditHash(authHeader), true, cache.DefaultExpiration) != nil {
		return
	}
	writeUPAuditEvent(c, upAuditEvent{
		Action:   "credential-validated",
		Category: upAuditCategoryAuthentication,
		Type:     "start",
		Outcome:  upAuditSuccess,
		UserID:   e.UserID,
		UserName: e.UserName,
		Labels:   map[string]string{"auth_method": "up-jwt"},
	})
}

// upAuditLinkAccess logs access to a public collection or file link: allowed
// when the link middleware stored its access context, denied when it aborted.
func upAuditLinkAccess(c *gin.Context, e upAuditEvent) {
	e.Action, e.Category = "shared-resource-accessed", upAuditCategoryWeb
	if v, ok := c.Get(auth.PublicAccessKey); ok {
		if ctx, ok := v.(ente.PublicAccessContext); ok {
			e.ResourceType, e.ResourceID = "collection", upAuditFormatID(ctx.CollectionID)
			upAuditLabel(&e, "link_id", upAuditFormatID(ctx.ID))
		}
	} else if v, ok := c.Get(auth.FileLinkAccessKey); ok {
		if ctx, ok := v.(*ente.FileLinkAccessContext); ok && ctx != nil {
			e.ResourceType, e.ResourceID = "file", upAuditFormatID(ctx.FileID)
			upAuditLabel(&e, "link_id", ctx.LinkID)
			upAuditLabel(&e, "owner_id", upAuditFormatID(ctx.OwnerID))
		}
	}
	if e.ResourceID != "" {
		// The client IP only feeds the de-duplication hash; it is never logged.
		key := upAuditHash(e.ResourceType, e.ResourceID, e.Labels["link_id"], network.GetClientIP(c), c.Request.UserAgent())
		if upAuditLinkAccessSeen.Add(key, true, cache.DefaultExpiration) != nil {
			return
		}
		e.Type, e.Outcome, e.Reason = "allowed", upAuditSuccess, ""
		writeUPAuditEvent(c, e)
		return
	}
	if !c.IsAborted() || e.StatusCode < http.StatusBadRequest {
		return
	}
	e.Type, e.Outcome = "denied", upAuditFailure
	if strings.HasPrefix(c.FullPath(), "/file-link/") {
		e.ResourceType = "file_link"
	} else {
		e.ResourceType = "collection_link"
	}
	writeUPAuditEvent(c, e)
}

func upAuditLogin(method string, nameFromRequest bool) upAuditRoute {
	return upAuditRoute{
		action: "user-login", category: upAuditCategoryAuthentication, eventType: "start",
		readRequest: nameFromRequest, readResponse: true,
		build: func(r upAuditRequest, e *upAuditEvent) {
			upAuditLabel(e, "auth_method", method)
			e.UserID = upAuditNum(r.resp, "id")
			if nameFromRequest {
				e.UserName = upAuditStr(r.req, "email")
			}
			// The first factor passed but no token is issued until the second.
			if e.Outcome == upAuditSuccess && (upAuditStr(r.resp, "twoFactorSessionID") != "" ||
				upAuditStr(r.resp, "twoFactorSessionIDV2") != "" || upAuditStr(r.resp, "passkeySessionID") != "") {
				e.Action = "user-login-first-factor"
			}
		},
	}
}

func upAuditShare(action string, eventType string) upAuditRoute {
	return upAuditRoute{
		action: action, category: upAuditCategoryIAM, eventType: eventType, readRequest: true,
		build: func(r upAuditRequest, e *upAuditEvent) {
			upAuditCollectionFromBody(r, e)
			e.TargetUserEmail = upAuditStr(r.req, "email")
			upAuditLabel(e, "role", upAuditStr(r.req, "role"))
		},
	}
}

func upAuditLink(action string, idField string, fromBody bool) upAuditRoute {
	resourceType := "collection"
	if idField == "fileID" {
		resourceType = "file"
	}
	return upAuditRoute{
		action: action, category: upAuditCategoryIAM, eventType: "change", readRequest: fromBody,
		build: func(r upAuditRequest, e *upAuditEvent) {
			e.ResourceType = resourceType
			if fromBody {
				e.ResourceID = upAuditFormatID(upAuditNum(r.req, idField))
			} else {
				e.ResourceID = r.c.Param(idField)
			}
		},
	}
}

func upAuditCollectionFromBody(r upAuditRequest, e *upAuditEvent) {
	e.ResourceType, e.ResourceID = "collection", upAuditFormatID(upAuditNum(r.req, "collectionID"))
}

func upAuditCollectionFromPath(r upAuditRequest, e *upAuditEvent) {
	id := r.c.Param("collectionID")
	if id == "" {
		id = r.c.Query("collectionID")
	}
	e.ResourceType, e.ResourceID = "collection", id
}

func upAuditFiles(e *upAuditEvent, ids []int64) {
	e.ResourceType = "file"
	n := len(ids)
	if n > upAuditMaxIDsInLabel {
		n = upAuditMaxIDsInLabel
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = strconv.FormatInt(ids[i], 10)
	}
	e.ResourceID = strings.Join(parts, ",")
	upAuditLabel(e, "resource_count", strconv.Itoa(len(ids)))
}

func upAuditLabel(e *upAuditEvent, key string, value string) {
	if value == "" {
		return
	}
	if e.Labels == nil {
		e.Labels = map[string]string{}
	}
	e.Labels[key] = value
}

func upAuditOutcomeOf(status int) upAuditOutcome {
	switch {
	case status < http.StatusBadRequest:
		return upAuditSuccess
	case status < http.StatusInternalServerError:
		return upAuditFailure
	default:
		return upAuditUnknown
	}
}

// upAuditPeekBody reads the request body for the audit and puts it back for
// the handler.
func upAuditPeekBody(c *gin.Context) []byte {
	body := c.Request.Body
	if body == nil || body == http.NoBody {
		return nil
	}
	buf, err := io.ReadAll(io.LimitReader(body, upAuditMaxRequestBody+1))
	c.Request.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(buf), body), body}
	if err != nil || len(buf) > upAuditMaxRequestBody {
		return nil
	}
	return buf
}

func upAuditParseJSON(b []byte) map[string]interface{} {
	if len(b) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber() // keep int64 IDs exact
	var m map[string]interface{}
	if dec.Decode(&m) != nil {
		return nil
	}
	return m
}

func upAuditNum(m map[string]interface{}, key string) int64 {
	if n, ok := m[key].(json.Number); ok {
		v, _ := n.Int64()
		return v
	}
	return 0
}

func upAuditNums(m map[string]interface{}, key string) []int64 {
	list, _ := m[key].([]interface{})
	ids := make([]int64, 0, len(list))
	for _, v := range list {
		if n, ok := v.(json.Number); ok {
			id, _ := n.Int64()
			ids = append(ids, id)
		}
	}
	return ids
}

func upAuditStr(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

// upAuditErrorMessage extracts the reason from {"error": "..."} or an
// ente.ApiError body, preferring its stable code over the free-text message.
func upAuditErrorMessage(body []byte) string {
	m := upAuditParseJSON(body)
	for _, key := range []string{"error", "code", "message"} {
		if s := upAuditStr(m, key); s != "" {
			return s
		}
	}
	return ""
}

func upAuditHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// upAuditWriter keeps a copy of the response body: in full (up to a limit)
// for routes that need the resource ID from it, otherwise only the start of
// error responses for the failure reason. The copy stays in memory and only
// parsed IDs and error reasons are logged, never tokens from the body.
type upAuditWriter struct {
	gin.ResponseWriter
	captureAll bool
	body       bytes.Buffer
}

func (w *upAuditWriter) Write(b []byte) (int, error) {
	w.capture(b)
	return w.ResponseWriter.Write(b)
}

func (w *upAuditWriter) WriteString(s string) (int, error) {
	w.capture([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

func (w *upAuditWriter) capture(b []byte) {
	limit := upAuditMaxErrorBody
	if w.captureAll {
		limit = upAuditMaxResponseBody
	} else if w.Status() < http.StatusBadRequest {
		return
	}
	if room := limit - w.body.Len(); room > 0 {
		if len(b) > room {
			b = b[:room]
		}
		w.body.Write(b)
	}
}
