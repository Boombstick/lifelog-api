package adminui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	rotationRequest = "request.json"
	rotationStatus  = "status.json"
	rotationLog     = "rotation.log"
	rotationSecrets = "secrets.txt"

	rotationConfirm = "СМЕНИТЬ"
)

type rotationRequestFile struct {
	ID          string     `json:"id"`
	RequestedBy string     `json:"requested_by"`
	RequestedAt *time.Time `json:"requested_at"`
}

type rotationState struct {
	ID          string     `json:"id"`
	State       string     `json:"state"`
	RequestedBy string     `json:"requested_by"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	ExitCode    *int       `json:"exit_code"`
	GitLab      string     `json:"gitlab"`
	Failed      []string   `json:"failed"`
}

func (h *Handler) rotationPath(name string) string { return filepath.Join(h.rotationDir, name) }

func (h *Handler) rotationPage(c *gin.Context) {
	h.renderRotation(c, http.StatusOK, "", c.Query("ok") != "")
}

func (h *Handler) renderRotation(c *gin.Context, code int, message string, queued bool) {
	data := gin.H{"Error": message, "Queued": queued, "Confirm": rotationConfirm}
	if h.rotationDir == "" {
		data["Disabled"] = true
		h.render(c, code, "rotate.html", data)
		return
	}

	if request, err := h.readRotationRequest(); err == nil {
		data["Pending"] = request
		data["Refresh"] = true
	}
	if state, err := h.readRotationState(); err == nil {
		data["Status"] = state
		if state.State == "running" {
			data["Refresh"] = true
		}
	}
	if _, err := os.Stat(h.rotationPath(rotationSecrets)); err == nil {
		data["HasSecrets"] = true
	}
	if log, err := os.ReadFile(h.rotationPath(rotationLog)); err == nil {
		data["Log"] = tail(string(log), 60)
	}
	h.render(c, code, "rotate.html", data)
}

func (h *Handler) startRotation(c *gin.Context) {
	if h.rotationDir == "" {
		h.renderRotation(c, http.StatusNotFound, "", false)
		return
	}
	if !h.checkCSRF(c) {
		h.renderRotation(c, http.StatusForbidden, "Форма устарела, откройте страницу заново.", false)
		return
	}
	if strings.TrimSpace(c.PostForm("confirm")) != rotationConfirm {
		h.renderRotation(c, http.StatusBadRequest, fmt.Sprintf("Для запуска впишите слово %s.", rotationConfirm), false)
		return
	}
	if _, err := h.readRotationRequest(); err == nil {
		h.renderRotation(c, http.StatusConflict, "Заявка уже ждёт запуска.", false)
		return
	}
	if state, err := h.readRotationState(); err == nil && state.State == "running" {
		h.renderRotation(c, http.StatusConflict, "Смена уже идёт — вторую запустить нельзя.", false)
		return
	}

	now := time.Now().UTC()
	request := rotationRequestFile{ID: uuid.NewString(), RequestedBy: principalOf(c).Name, RequestedAt: &now}
	if err := writeFileAtomic(h.rotationPath(rotationRequest), request); err != nil {
		h.renderRotation(c, http.StatusInternalServerError, "Не удалось записать заявку: "+err.Error(), false)
		return
	}
	c.Redirect(http.StatusSeeOther, h.prefix+"/rotate?ok=1")
}

func (h *Handler) revealSecrets(c *gin.Context) {
	if h.rotationDir == "" || !h.checkCSRF(c) {
		h.renderRotation(c, http.StatusForbidden, "Форма устарела, откройте страницу заново.", false)
		return
	}
	path := h.rotationPath(rotationSecrets)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		h.renderRotation(c, http.StatusGone, "Новые значения уже были показаны и удалены.", false)
		return
	}
	if err != nil {
		h.renderRotation(c, http.StatusInternalServerError, "Не удалось прочитать значения: "+err.Error(), false)
		return
	}
	if err := os.Remove(path); err != nil {
		h.renderRotation(c, http.StatusInternalServerError, "Значения не удалось удалить, поэтому они не показаны: "+err.Error(), false)
		return
	}

	c.Header("Cache-Control", "no-store")
	h.render(c, http.StatusOK, "secrets.html", gin.H{"Secrets": strings.TrimRight(string(content), "\n")})
}

func (h *Handler) readRotationRequest() (*rotationRequestFile, error) {
	var request rotationRequestFile
	if err := readJSON(h.rotationPath(rotationRequest), &request); err != nil {
		return nil, err
	}
	return &request, nil
}

func (h *Handler) readRotationState() (*rotationState, error) {
	var state rotationState
	if err := readJSON(h.rotationPath(rotationStatus), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func readJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

func writeFileAtomic(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func tail(text string, lines int) string {
	all := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}
