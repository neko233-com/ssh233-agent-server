package audit

import (
	"log/slog"

	"github.com/neko233/ssh233-agent-server/internal/models"
	"github.com/neko233/ssh233-agent-server/internal/store"
)

type Logger struct {
	store  *store.Store
	logger *slog.Logger
}

func New(st *store.Store) *Logger {
	return &Logger{
		store:  st,
		logger: slog.Default().With("component", "audit"),
	}
}

func (l *Logger) Log(tenantID, action, username, userID, hostID, sessionID, detail, clientIP string) {
	entry := &models.AuditLog{
		TenantID:  tenantID,
		SessionID: sessionID,
		UserID:    userID,
		Username:  username,
		HostID:    hostID,
		Action:    action,
		Detail:    detail,
		ClientIP:  clientIP,
	}
	if err := l.store.WriteAudit(entry); err != nil {
		l.logger.Error("write audit log", "error", err)
	}
	l.logger.Info("audit", "action", action, "user", username, "detail", detail)
}

func (l *Logger) RecordCommand(sessionID, command, output string, exitCode *int) {
	rec := &models.CommandRecord{
		SessionID: sessionID,
		Command:   command,
		Output:    output,
		ExitCode:  exitCode,
	}
	if err := l.store.RecordCommand(rec); err != nil {
		l.logger.Error("record command", "error", err)
	}
}
