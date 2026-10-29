package middleware

// Unplugged security event logging (UNP-8993), output side: a dedicated
// logger that always writes ECS JSON lines to stdout, independently of the
// main logrus logger (which goes to log-file outside local). The cluster log
// agent ships every line with labels.log_type=security to the security data
// stream.
//
// service.name defaults to "museum" and can be changed with the
// security-audit.service-name config key (ENTE_SECURITY_AUDIT_SERVICE_NAME).
// service.environment is the ENVIRONMENT variable.

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type upAuditOutcome string

const (
	upAuditSuccess upAuditOutcome = "success"
	upAuditFailure upAuditOutcome = "failure"
	upAuditUnknown upAuditOutcome = "unknown"
)

const (
	upAuditLoggerName      = "SECURITY_AUDIT"
	upAuditECSVersion      = "8.11.0"
	upAuditTimestampLayout = "2006-01-02T15:04:05.000Z"
	upAuditRedacted        = "[REDACTED]"
	upAuditMaxValueLength  = 512
)

// upAuditEvent is one security event. Empty values are omitted.
type upAuditEvent struct {
	Action          string
	Category        string
	Type            string
	Outcome         upAuditOutcome
	UserID          int64
	UserName        string
	TargetUserID    int64
	TargetUserName  string
	TargetUserEmail string
	ResourceType    string
	ResourceID      string
	Reason          string
	StatusCode      int
	Labels          map[string]string
}

var (
	upAuditLogger = newUPAuditLogger()

	upAuditServiceOnce sync.Once
	upAuditService     string
	upAuditEnvironment string
)

func newUPAuditLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(os.Stdout)
	l.SetFormatter(&upAuditECSFormatter{})
	l.SetLevel(logrus.InfoLevel)
	return l
}

func upAuditServiceInfo() (string, string) {
	upAuditServiceOnce.Do(func() {
		upAuditService = viper.GetString("security-audit.service-name")
		if upAuditService == "" {
			upAuditService = "museum"
		}
		upAuditEnvironment = os.Getenv("ENVIRONMENT")
		if upAuditEnvironment == "" {
			upAuditEnvironment = "local"
		}
	})
	return upAuditService, upAuditEnvironment
}

// writeUPAuditEvent writes e, adding request context from c.
func writeUPAuditEvent(c *gin.Context, e upAuditEvent) {
	if e.Outcome == "" {
		e.Outcome = upAuditUnknown
	}
	service, env := upAuditServiceInfo()
	fields := logrus.Fields{
		"labels.log_type":     "security",
		"event.kind":          "event",
		"event.dataset":       "security." + service,
		"event.action":        e.Action,
		"event.outcome":       string(e.Outcome),
		"service.name":        service,
		"service.environment": env,
	}
	upAuditSet(fields, "event.category", e.Category)
	upAuditSet(fields, "event.type", e.Type)
	upAuditSet(fields, "event.reason", upAuditSanitize(e.Reason))
	upAuditSet(fields, "user.name", e.UserName)
	upAuditSet(fields, "user.target.name", e.TargetUserName)
	upAuditSet(fields, "user.target.email", e.TargetUserEmail)
	upAuditSet(fields, "labels.resource_type", e.ResourceType)
	upAuditSet(fields, "labels.resource_id", e.ResourceID)
	upAuditSet(fields, "user.id", upAuditFormatID(e.UserID))
	upAuditSet(fields, "user.target.id", upAuditFormatID(e.TargetUserID))
	for k, v := range e.Labels {
		upAuditSet(fields, "labels."+k, upAuditSanitize(v))
	}
	if e.StatusCode != 0 {
		fields["http.response.status_code"] = e.StatusCode
	}
	if c != nil {
		if c.Request != nil {
			upAuditSet(fields, "user_agent.original", c.Request.UserAgent())
			upAuditSet(fields, "http.request.method", c.Request.Method)
		}
		// The route template (e.g. /files/share-url/:fileID): never carries
		// query-string tokens.
		upAuditSet(fields, "url.path", c.FullPath())
		if c.Writer != nil {
			upAuditSet(fields, "trace.id", requestid.Get(c))
		}
	}
	upAuditLogger.WithFields(fields).Info(e.Action + " " + string(e.Outcome))
}

// upAuditECSFormatter renders an entry as one ECS JSON line, nesting the
// dotted field names into objects.
type upAuditECSFormatter struct{}

func (f *upAuditECSFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	root := map[string]interface{}{}
	put := func(key string, value interface{}) {
		parts := strings.Split(key, ".")
		node := root
		for _, p := range parts[:len(parts)-1] {
			child, ok := node[p].(map[string]interface{})
			if !ok {
				child = map[string]interface{}{}
				node[p] = child
			}
			node = child
		}
		node[parts[len(parts)-1]] = value
	}
	put("@timestamp", entry.Time.UTC().Format(upAuditTimestampLayout))
	put("message", entry.Message)
	put("log.level", entry.Level.String())
	put("log.logger", upAuditLoggerName)
	put("ecs.version", upAuditECSVersion)
	for k, v := range entry.Data {
		put(k, v)
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

var (
	upAuditJWTPattern    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
	upAuditBearerPattern = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	// key=value or "key": "value" pairs whose key names a secret
	upAuditSecretPattern = regexp.MustCompile(`(?i)\b((?:x-auth-)?(?:access[_-]?)?token|password|passwd|pass[_-]?hash|secret|client[_-]?secret|ott|otp|totp|authorization|cookie|api[_-]?key|srp[_-]?m1)(["']?\s*[:=]\s*["']?)[^\s"',;&]+`)
)

// upAuditSanitize removes credentials from free text and caps its length. It
// is a safety net; callers never pass secrets on purpose.
func upAuditSanitize(s string) string {
	if s == "" {
		return s
	}
	s = upAuditJWTPattern.ReplaceAllString(s, upAuditRedacted)
	s = upAuditBearerPattern.ReplaceAllString(s, "${1}"+upAuditRedacted)
	s = upAuditSecretPattern.ReplaceAllString(s, "${1}${2}"+upAuditRedacted)
	if len(s) > upAuditMaxValueLength {
		s = s[:upAuditMaxValueLength] + "..."
	}
	return s
}

func upAuditSet(fields logrus.Fields, key string, value string) {
	if value != "" {
		fields[key] = value
	}
}

func upAuditFormatID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
