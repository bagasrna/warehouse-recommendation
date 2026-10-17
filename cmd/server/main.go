package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"example.com/simpul-warehouse-selection/internal/assets"
	"example.com/simpul-warehouse-selection/internal/httpapi"
	"example.com/simpul-warehouse-selection/internal/insights"
	"example.com/simpul-warehouse-selection/internal/selection"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	catalog, err := assets.Catalog()
	if err != nil {
		log.Error("invalid catalog", "error", err)
		os.Exit(1)
	}
	penalty := selection.DefaultPenaltyKMEq
	if value, ok := os.LookupEnv("DISPATCH_PENALTY_KM_EQ"); ok {
		penalty, err = strconv.Atoi(value)
		if err != nil || penalty < 0 || penalty > 10000 {
			log.Error("invalid DISPATCH_PENALTY_KM_EQ (expected integer 0..10000)", "value", value)
			os.Exit(1)
		}
	}
	engine, err := selection.New(catalog, penalty)
	if err != nil {
		log.Error("selection engine initialization failed", "error", err)
		os.Exit(1)
	}
	web, err := assets.Web()
	if err != nil {
		log.Error("web files unavailable", "error", err)
		os.Exit(1)
	}
	history, err := assets.HistoryCSV()
	if err != nil {
		log.Error("history missing", "error", err)
		os.Exit(1)
	}
	historyData, err := insights.ParseCSV(history)
	if err != nil {
		log.Error("history analysis failed", "error", err)
		os.Exit(1)
	}
	report := historyData.Report()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           httpapi.New(engine, catalog, report, historyData, web, log).Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("shutdown failed", "error", err)
		}
	}()
	log.Info("server started", "port", port, "warehouses", len(catalog.Warehouses), "products", len(catalog.Products))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server exited", "error", err)
		os.Exit(1)
	}
}
