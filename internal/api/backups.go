package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Pegasus8/piworker/internal/backup"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// BackupsOptions wires the automatic backups (ADR-0032).
type BackupsOptions struct {
	Service *backup.Service
	// Auth confirms the account password before a restore or a change of
	// the backup directory; nil while authentication is disabled.
	Auth *AuthHandler
	// Restart asks the process to stop so it starts again and applies a
	// staged restore.
	Restart func()
	Logger  zerolog.Logger
}

// BackupsHandler serves /api/backups.
type BackupsHandler struct {
	opts BackupsOptions
}

// NewBackupsHandler creates the handler.
func NewBackupsHandler(opts BackupsOptions) *BackupsHandler {
	if opts.Restart == nil {
		opts.Restart = func() {}
	}
	return &BackupsHandler{opts: opts}
}

// RegisterRoutes registers the routes.
func (h *BackupsHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/backups", h.Overview).Methods(http.MethodGet)
	r.HandleFunc("/api/backups", h.BackUpNow).Methods(http.MethodPost)
	r.HandleFunc("/api/backups/settings", h.SaveSettings).Methods(http.MethodPut)
	r.HandleFunc("/api/backups/{name}", h.Delete).Methods(http.MethodDelete)
	r.HandleFunc("/api/backups/{name}/restore", h.Restore).Methods(http.MethodPost)
}

// BackupsOverview is GET /api/backups: the settings, where backups go, the
// last outcome, the next scheduled run and the backups, newest first.
type BackupsOverview struct {
	Settings       backup.Settings `json:"settings"`
	Dir            string          `json:"dir"`
	DefaultDir     string          `json:"defaultDir"`
	Status         backup.Status   `json:"status"`
	NextRun        *time.Time      `json:"nextRun"`
	Backups        []backup.Info   `json:"backups"`
	RestorePending bool            `json:"restorePending"`
}

// Overview answers BackupsOverview.
func (h *BackupsHandler) Overview(w http.ResponseWriter, r *http.Request) {
	svc := h.opts.Service
	set, err := svc.Settings()
	if err != nil {
		h.opts.Logger.Error().Err(err).Msg("Cannot read the backup settings")
		writeError(w, http.StatusInternalServerError, "Cannot read the backup settings")
		return
	}
	dir, _ := svc.Dir()
	list, err := svc.List()
	if err != nil {
		h.opts.Logger.Warn().Err(err).Msg("Cannot list backups")
	}
	for i := range list {
		list[i].Manifest.Files = nil
	}
	if list == nil {
		list = []backup.Info{}
	}
	ov := BackupsOverview{
		Settings: set, Dir: dir, DefaultDir: svc.DefaultDir(), Status: svc.Status(), Backups: list,
		RestorePending: svc.RestorePending(),
	}
	if next := svc.NextRun(); !next.IsZero() {
		ov.NextRun = &next
	}
	writeSuccess(w, http.StatusOK, ov, "")
}

// BackUpNow makes a manual backup.
func (h *BackupsHandler) BackUpNow(w http.ResponseWriter, r *http.Request) {
	info, _, err := h.opts.Service.BackUp(r.Context(), backup.ReasonManual)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The backup failed: "+err.Error())
		return
	}
	info.Manifest.Files = nil
	writeSuccess(w, http.StatusCreated, info, "")
}

// BackupSettingsRequest is PUT /api/backups/settings's body: the settings,
// and the account password when the directory changes.
type BackupSettingsRequest struct {
	backup.Settings
	Password string `json:"password"`
}

// SaveSettings stores the settings. Changing the directory decides where
// copies of every secret and of the CA key are written, so it needs the
// account password (when authentication is enabled).
func (h *BackupsHandler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req BackupSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	current, err := h.opts.Service.Settings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Cannot read the backup settings")
		return
	}
	if filepath.Clean(req.Dir) != filepath.Clean(current.Dir) && h.opts.Auth != nil {
		if !h.opts.Auth.verifyStepUp(w, r, req.Password) {
			return
		}
	}
	if err := h.opts.Service.SaveSettings(req.Settings); err != nil {
		if errors.Is(err, backup.ErrInvalidSettings) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.opts.Logger.Error().Err(err).Msg("Cannot save the backup settings")
		writeError(w, http.StatusInternalServerError, "Cannot save the backup settings")
		return
	}
	saved, _ := h.opts.Service.Settings()
	h.opts.Logger.Info().Bool("enabled", saved.Enabled).Str("time", saved.Time).Str("dir", saved.Dir).Msg("Backup settings changed")
	writeSuccess(w, http.StatusOK, saved, "")
}

// Delete removes a backup.
func (h *BackupsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if err := h.opts.Service.Delete(name); err != nil {
		if errors.Is(err, backup.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Backup not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "Cannot delete the backup: "+err.Error())
		return
	}
	h.opts.Logger.Info().Str("file", name).Msg("Backup deleted")
	writeSuccess(w, http.StatusOK, nil, "")
}

// BackupRestoreRequest confirms the account password.
type BackupRestoreRequest struct {
	Password string `json:"password"`
}

// BackupRestoreResponse: the backup that will be restored; PiWorker is
// restarting to apply it.
type BackupRestoreResponse struct {
	Manifest   backup.Manifest `json:"manifest"`
	Restarting bool            `json:"restarting"`
}

// Restore stages a backup (after backing up the current state) and asks
// the process to restart; the restore is applied before the database is
// opened again. It replaces accounts, flows and secrets, so it needs the
// account password (when authentication is enabled).
func (h *BackupsHandler) Restore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req BackupRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if h.opts.Auth != nil && !h.opts.Auth.verifyStepUp(w, r, req.Password) {
		return
	}
	name := mux.Vars(r)["name"]
	m, err := h.opts.Service.StageRestore(r.Context(), name)
	switch {
	case errors.Is(err, backup.ErrNotFound):
		writeError(w, http.StatusNotFound, "Backup not found")
		return
	case errors.Is(err, backup.ErrInvalidBackup), errors.Is(err, backup.ErrNewerBackup):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		h.opts.Logger.Error().Err(err).Msg("Cannot stage the restore")
		writeError(w, http.StatusInternalServerError, "Cannot restore: "+err.Error())
		return
	}
	m.Files = nil
	writeSuccess(w, http.StatusAccepted, BackupRestoreResponse{Manifest: m, Restarting: true}, "")
	h.opts.Restart()
}
