package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"wechat-hub/auth"
	"wechat-hub/hub"
	"wechat-hub/storage"
)

const maxUploadSize = 20 << 10 << 10 // 20MB

type (
	HttpHandler struct {
		*http.ServeMux
		maxUploadSize    int
		maxUploadSizeStr string
		storage          storage.Storage
		sender           *MsgSender
		auth             *auth.Manager
		member           hub.MemberManager
	}
	HttpHandlerOption = func(sender *HttpHandler)

	httpResult[T any] struct {
		Code int    `json:"code"` // 0表示成功
		Msg  string `json:"msg"`  //
		Data T      `json:"data"`
	}
)

func WithMaxUploadSize(size int) HttpHandlerOption {
	return func(handler *HttpHandler) {
		handler.maxUploadSize = maxUploadSize
		units := []string{"B", "KB", "MB", "GB"}
		if size < 1024 {
			handler.maxUploadSizeStr = fmt.Sprintf("%d%s", size, units[0])
		}
		divisor := math.Log(float64(size)) / math.Log(1024)
		unitIndex := int(math.Floor(divisor))
		value := float64(size) / math.Pow(1024, float64(unitIndex))
		handler.maxUploadSizeStr = fmt.Sprintf("%.2f%s", value, units[unitIndex])
	}
}
func WithBaseAuth(manager *auth.Manager) HttpHandlerOption {
	return func(handler *HttpHandler) {
		handler.auth = manager
	}
}

func NewHttpHandler(storage storage.Storage, member hub.MemberManager, sender *MsgSender, options ...HttpHandlerOption) *HttpHandler {
	h := &HttpHandler{
		ServeMux:         http.NewServeMux(),
		maxUploadSize:    maxUploadSize,
		maxUploadSizeStr: "20.00MB",
		storage:          storage,
		sender:           sender,
		member:           member,
	}
	for _, option := range options {
		option(h)
	}
	h.HandleFunc("/health", h.health)
	h.HandleFunc("/upload", h.upload)
	h.HandleFunc("/resource", h.resource)
	h.HandleFunc("/msg/send", h.sendMsg)
	h.HandleFunc("/group", h.group)
	return h
}

func (h *HttpHandler) ListenAndServe(port int) {
	slog.Info("HttpHandler listening on", "port", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), h); err != nil {
		slog.Error("HttpHandler ListenAndServe", "err", err)
	}
}

func (h *HttpHandler) Error(w http.ResponseWriter, error string, code int) {
	jsonData, err := json.Marshal(httpResult[string]{
		Code: code,
		Msg:  error,
	})
	if err != nil {
		slog.Error("HttpHandler marshal Error", "err", err)
		http.Error(w, error, code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(jsonData)
}

func (h *HttpHandler) Success(w http.ResponseWriter, data any) {
	jsonData, err := json.Marshal(httpResult[any]{
		Code: 0,
		Msg:  "OK",
		Data: data,
	})
	if err != nil {
		h.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(jsonData)
}

func (h *HttpHandler) checkAuth(r *http.Request) bool {
	if h.auth != nil {
		username, password, ok := r.BasicAuth()
		if !ok {
			return false
		}
		return h.auth.CheckUser(username, password)
	}
	return true
}

func (h *HttpHandler) health(w http.ResponseWriter, r *http.Request) {
	if h.sender.Bot.Alive() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("UP"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("DOWN"))
	}
}

// 上传资源
func (h *HttpHandler) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	if !h.checkAuth(r) {
		h.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxUploadSize))
	err := r.ParseMultipartForm(int64(maxUploadSize))
	if err != nil {
		slog.Error("HttpHandler upload parse multipart form", "err", err)
		if errors.Is(err, multipart.ErrMessageTooLarge) {
			h.Error(w, fmt.Sprintf("File is too large, maximum allowed size is %s.", h.maxUploadSizeStr), http.StatusRequestEntityTooLarge)
			return
		}
		h.Error(w, "Error parsing request body.", http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		slog.Error("HttpHandler upload parse form file", "err", err)
		h.Error(w, "Error retrieving the file.", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = file.Close()
	}()

	filename := filepath.Base(r.FormValue("filename"))
	if filename == "." || filename == ".." || filename == string(os.PathSeparator) {
		filename = handler.Filename
	}

	writer, f, err := h.storage.Writer(filename)
	if err != nil {
		slog.Error("HttpHandler upload create file", "err", err)
		h.Error(w, "Error creating file on server.", http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = writer.Close()
	}()
	if _, err := io.Copy(writer, file); err != nil {
		slog.Error("HttpHandler upload write file", "err", err)
		h.Error(w, "Error writing file to server.", http.StatusInternalServerError)
		return
	}
	h.Success(w, "RESOURCE:"+f)
}

func (h *HttpHandler) resource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	resource := r.URL.Query().Get("resource")
	if resource == "" {
		h.Error(w, "Invalid resource", http.StatusBadRequest)
		return
	}
	reader, err := h.storage.Reader(resource)
	if err != nil {
		slog.Error("HttpHandler resource", "err", err)
		if errors.Is(err, fs.ErrNotExist) {
			h.Error(w, "Resource not found", http.StatusNotFound)
			return
		}
		h.Error(w, "Error reading file from server.", http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = reader.Close()
	}()
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

// 发送消息
func (h *HttpHandler) sendMsg(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	if !h.checkAuth(r) {
		h.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	contentType := r.Header.Get("Content-Type")
	var msg hub.SendMsgCommand
	switch {
	case contentType == "application/json":
		defer func() {
			_ = r.Body.Close()
		}()
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			slog.Error("HttpHandler sendMsg decode json", "err", err)
			h.Error(w, "Error parsing request body.", http.StatusBadRequest)
			return
		}
	case contentType == "application/x-www-form-urlencoded":
		// 解析 form 表单数据
		err := r.ParseForm()
		if err != nil {
			slog.Error("HttpHandler sendMsg parse form", "err", err)
			h.Error(w, "Error parsing request body.", http.StatusBadRequest)
			return
		}
		msg.Gid = r.Form.Get("gid")
		msg.Body = r.Form.Get("body")
		msg.Filename = r.Form.Get("filename")
		msg.Prompt = r.Form.Get("prompt")
		msg.Type, err = strconv.Atoi(r.Form.Get("type"))
		if err != nil {
			slog.Error("HttpHandler sendMsg parse type", "err", err)
			h.Error(w, "Error parsing msg type.", http.StatusBadRequest)
			return
		}
	default:
		h.Error(w, "Unsupported Content-Type", http.StatusUnsupportedMediaType)
		return
	}
	if err := h.sender.SendMsg(&msg); err != nil {
		h.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Success(w, "OK")
}

func (h *HttpHandler) group(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	gid := r.URL.Query().Get("gid")
	if gid == "" {
		h.Error(w, "Invalid gid", http.StatusBadRequest)
		return
	}
	if !h.checkAuth(r) {
		h.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	userMap, err := h.member.GetGroupUsers(gid)
	if err != nil {
		slog.Error("HttpHandler group", "err", err)
		h.Error(w, "Error reading group users from server.", http.StatusInternalServerError)
		return
	}
	users := make([]hub.GroupUser, 0, len(userMap))
	for _, user := range userMap {
		users = append(users, user)
	}
	h.Success(w, users)
}
