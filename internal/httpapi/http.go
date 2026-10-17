package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"

	"example.com/simpul-warehouse-selection/internal/domain"
	"example.com/simpul-warehouse-selection/internal/insights"
	"example.com/simpul-warehouse-selection/internal/selection"
)

const maxOrderBody = 64 << 10 // 64 KiB

type Server struct {
	engine  *selection.Engine
	catalog domain.Catalog
	insight insights.Report
	history *insights.History
	web     fs.FS
	logger  *slog.Logger
}

func New(engine *selection.Engine, catalog domain.Catalog, report insights.Report, history *insights.History, web fs.FS, logger *slog.Logger) *Server {
	return &Server{engine: engine, catalog: catalog, insight: report, history: history, web: web, logger: logger}
}

type errorEnvelope struct {
	Error struct {
		Code    string                      `json:"code"`
		Message string                      `json:"message"`
		Details []selection.ValidationError `json:"details,omitempty"`
	} `json:"error"`
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, status int, code, message string, details []selection.ValidationError) {
	e := errorEnvelope{}
	e.Error.Code = code
	e.Error.Message = message
	e.Error.Details = details
	respondJSON(w, status, e)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, struct {
			domain.Catalog
			Policy struct {
				PenaltyKMEq int `json:"penalty_km_equivalent"`
			} `json:"policy"`
		}{Catalog: s.catalog, Policy: struct {
			PenaltyKMEq int `json:"penalty_km_equivalent"`
		}{PenaltyKMEq: s.engine.PenaltyKMEq()}})
	})
	mux.HandleFunc("GET /api/v1/insights", func(w http.ResponseWriter, r *http.Request) { respondJSON(w, http.StatusOK, s.insight) })
	mux.HandleFunc("POST /api/v1/recommendations", s.recommend)
	mux.HandleFunc("/api/v1/recommendations", func(w http.ResponseWriter, r *http.Request) {
		respondError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Use POST for recommendations", nil)
	})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			respondError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			return
		}
		http.FileServer(http.FS(s.web)).ServeHTTP(w, r)
	}))
	return mux
}

func (s *Server) recommend(w http.ResponseWriter, r *http.Request) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxOrderBody)
	var req struct {
		Items            []domain.OrderItem `json:"items"`
		DeliverySchedule *insights.Schedule `json:"delivery_schedule,omitempty"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.decodeError(w, err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "Expected exactly one JSON object", nil)
		} else {
			s.decodeError(w, err)
		}
		return
	}
	result, err := s.engine.RecommendScheduled(req.Items, req.DeliverySchedule, s.history)
	if err != nil {
		var validation *selection.ValidationError
		if errors.As(err, &validation) {
			respondError(w, http.StatusUnprocessableEntity, "INVALID_ORDER", "Order validation failed", []selection.ValidationError{*validation})
			return
		}
		s.logger.Error("warehouse recommendation failed", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred", nil)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) decodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		respondError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Order JSON exceeds 64 KiB", nil)
		return
	}
	respondError(w, http.StatusBadRequest, "BAD_REQUEST", "Malformed JSON or unsupported field/type", nil)
}
