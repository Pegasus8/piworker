package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Pegasus8/piworker/internal/backup"
	"github.com/gorilla/mux"
)

// MaxBackupUploadSize bounds a backup uploaded to restore. The JSON body
// carries it in base64 (the request guard admits only JSON).
const MaxBackupUploadSize = 64 << 20

// registerRemoteRoutes registers the remote destination, the encrypted
// download and the restores from an uploaded file.
func (h *BackupsHandler) registerRemoteRoutes(r *mux.Router) {
	r.HandleFunc("/api/backups/remote", h.SaveRemote).Methods(http.MethodPut)
	r.HandleFunc("/api/backups/remote/test", h.TestRemote).Methods(http.MethodPost)
	r.HandleFunc("/api/backups/remote/list", h.ListRemote).Methods(http.MethodGet)
	r.HandleFunc("/api/backups/remote/{name}/restore", h.RestoreRemote).Methods(http.MethodPost)
	r.HandleFunc("/api/backups/upload", h.Upload).Methods(http.MethodPost)
	r.HandleFunc("/api/backups/{name}/download", h.Download).Methods(http.MethodPost)
	r.HandleFunc("/api/auth/setup/restore", h.SetupRestore).Methods(http.MethodPost)
}

// writeBackupError maps the backup errors to HTTP statuses.
func (h *BackupsHandler) writeBackupError(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, backup.ErrNotFound):
		writeError(w, http.StatusNotFound, "Backup not found")
	case errors.Is(err, backup.ErrInvalidBackup), errors.Is(err, backup.ErrNewerBackup),
		errors.Is(err, backup.ErrWrongPassphrase), errors.Is(err, backup.ErrWeakPassphrase),
		errors.Is(err, backup.ErrInvalidSettings):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, backup.ErrRemote):
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		h.opts.Logger.Error().Err(err).Msg(what)
		writeError(w, http.StatusInternalServerError, what+": "+err.Error())
	}
}

// RemoteRequest is the remote destination with its write-only secrets
// (empty keeps the stored ones) and the account password.
type RemoteRequest struct {
	backup.RemoteSettings
	SecretKey  string `json:"secretKey"`
	Passphrase string `json:"passphrase"`
	Password   string `json:"password"`
}

func (h *BackupsHandler) readJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// stepUp confirms the account password when authentication is enabled.
func (h *BackupsHandler) stepUp(w http.ResponseWriter, r *http.Request, password string) bool {
	return h.opts.Auth == nil || h.opts.Auth.verifyStepUp(w, r, password)
}

// SaveRemote stores the remote destination. It decides where encrypted
// copies of every secret go: the account password is confirmed.
func (h *BackupsHandler) SaveRemote(w http.ResponseWriter, r *http.Request) {
	var req RemoteRequest
	if !h.readJSON(w, r, MaxRequestBodySize, &req) || !h.stepUp(w, r, req.Password) {
		return
	}
	if err := h.opts.Service.SaveRemote(req.RemoteSettings, req.SecretKey, req.Passphrase); err != nil {
		h.writeBackupError(w, err, "Cannot save the remote destination")
		return
	}
	st, err := h.opts.Service.RemoteState()
	if err != nil {
		h.writeBackupError(w, err, "Cannot read the remote destination")
		return
	}
	h.opts.Logger.Info().Bool("enabled", st.Settings.Enabled).Str("endpoint", st.Settings.Endpoint).Str("bucket", st.Settings.Bucket).Msg("Remote backup destination changed")
	writeSuccess(w, http.StatusOK, st, "")
}

// TestRemote checks a destination. The stored secret key is used only for
// the stored endpoint and key ID, so it is never signed into a request to
// a host the admin did not save.
func (h *BackupsHandler) TestRemote(w http.ResponseWriter, r *http.Request) {
	var req RemoteRequest
	if !h.readJSON(w, r, MaxRequestBodySize, &req) {
		return
	}
	if req.SecretKey == "" {
		st, err := h.opts.Service.RemoteState()
		if err != nil {
			h.writeBackupError(w, err, "Cannot read the remote destination")
			return
		}
		same := strings.TrimSpace(req.Endpoint) == st.Settings.Endpoint && strings.TrimSpace(req.AccessKeyID) == st.Settings.AccessKeyID
		if !st.HasSecretKey || !same {
			writeError(w, http.StatusBadRequest, "Enter the secret access key")
			return
		}
	}
	if err := h.opts.Service.TestRemote(r.Context(), req.RemoteSettings, req.SecretKey); err != nil {
		h.writeBackupError(w, err, "Cannot reach the remote destination")
		return
	}
	writeSuccess(w, http.StatusOK, nil, "")
}

// ListRemote lists the copies at the remote destination.
func (h *BackupsHandler) ListRemote(w http.ResponseWriter, r *http.Request) {
	list, err := h.opts.Service.ListRemote(r.Context())
	if err != nil {
		h.writeBackupError(w, err, "Cannot list the remote backups")
		return
	}
	if list == nil {
		list = []backup.Info{}
	}
	writeSuccess(w, http.StatusOK, list, "")
}

// RestoreRemote downloads, decrypts and stages a remote copy, then restarts.
func (h *BackupsHandler) RestoreRemote(w http.ResponseWriter, r *http.Request) {
	var req BackupRestoreRequest
	if !h.readJSON(w, r, MaxRequestBodySize, &req) || !h.stepUp(w, r, req.Password) {
		return
	}
	m, err := h.opts.Service.StageRemoteRestore(r.Context(), mux.Vars(r)["name"])
	if err != nil {
		h.writeBackupError(w, err, "Cannot restore")
		return
	}
	h.restarting(w, m)
}

func (h *BackupsHandler) restarting(w http.ResponseWriter, m backup.Manifest) {
	m.Files = nil
	writeSuccess(w, http.StatusAccepted, BackupRestoreResponse{Manifest: m, Restarting: true}, "")
	h.opts.Restart()
}

// BackupDownloadRequest: the passphrase that encrypts the copy and the
// account password.
type BackupDownloadRequest struct {
	Passphrase string `json:"passphrase"`
	Password   string `json:"password"`
}

// Download answers a local backup encrypted with the passphrase (age): a
// copy that leaves the device is always encrypted. It carries every
// secret, so it needs the account password, and is refused while
// authentication is disabled.
func (h *BackupsHandler) Download(w http.ResponseWriter, r *http.Request) {
	var req BackupDownloadRequest
	if !h.readJSON(w, r, MaxRequestBodySize, &req) {
		return
	}
	if h.opts.Auth == nil {
		writeError(w, http.StatusForbidden, "Downloading a backup requires authentication, which is disabled on this installation")
		return
	}
	if err := backup.CheckPassphrase(req.Passphrase); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.opts.Auth.verifyStepUp(w, r, req.Password) {
		return
	}
	name := mux.Vars(r)["name"]
	if !backup.IsBackupName(name) {
		writeError(w, http.StatusNotFound, "Backup not found")
		return
	}
	// Encrypt into memory first so a failure is still an error answer.
	var buf bytes.Buffer
	if err := h.opts.Service.Download(name, req.Passphrase, &buf); err != nil {
		h.writeBackupError(w, err, "Cannot download the backup")
		return
	}
	h.opts.Logger.Info().Str("file", name).Msg("Backup downloaded (encrypted)")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+backup.EncryptedSuffix+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// BackupUploadRequest is a backup file in base64: a plain archive or an
// encrypted copy with its passphrase.
type BackupUploadRequest struct {
	Data       string `json:"data"`
	Passphrase string `json:"passphrase"`
	Password   string `json:"password"`
	Code       string `json:"code"`
}

func (h *BackupsHandler) readUpload(w http.ResponseWriter, r *http.Request) (BackupUploadRequest, []byte, bool) {
	var req BackupUploadRequest
	if !h.readJSON(w, r, int64(base64.StdEncoding.EncodedLen(MaxBackupUploadSize))+64<<10, &req) {
		return req, nil, false
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusBadRequest, "Invalid request body: expected the backup file in base64 as data")
		return req, nil, false
	}
	return req, data, true
}

// Upload restores an uploaded backup on a running installation (after
// backing up the current state), then restarts.
func (h *BackupsHandler) Upload(w http.ResponseWriter, r *http.Request) {
	req, data, ok := h.readUpload(w, r)
	if !ok || !h.stepUp(w, r, req.Password) {
		return
	}
	m, err := h.opts.Service.StageUpload(r.Context(), bytes.NewReader(data), req.Passphrase, true)
	if err != nil {
		h.writeBackupError(w, err, "Cannot restore")
		return
	}
	h.restarting(w, m)
}

// SetupRestore restores a backup on the setup page of a fresh installation
// instead of creating an account. The setup code authorises it; it is
// used up only once the backup was accepted.
func (h *BackupsHandler) SetupRestore(w http.ResponseWriter, r *http.Request) {
	if h.opts.Auth == nil {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	req, data, ok := h.readUpload(w, r)
	if !ok || !h.opts.Auth.checkSetupCode(w, r, req.Code) {
		return
	}
	m, err := h.opts.Service.StageUpload(r.Context(), bytes.NewReader(data), req.Passphrase, false)
	if err != nil {
		h.writeBackupError(w, err, "Cannot restore")
		return
	}
	if err := h.opts.Auth.consumeSetupCode(r, req.Code); err != nil {
		h.opts.Logger.Error().Err(err).Msg("Cannot use up the setup code")
	}
	h.opts.Logger.Warn().Time("created", m.Created).Msg("Backup restored from the setup page")
	h.restarting(w, m)
}
