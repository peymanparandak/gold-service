package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// GoldPrice is the response and DB model.
type GoldPrice struct {
	Name      string `json:"name"`
	Price     int64  `json:"price"`
	FetchedAt string `json:"fetchedAt"`
	Stale     bool   `json:"stale"`
}

// BrsApiResponse is the shape of the BRS API response.
type BrsApiResponse struct {
	Gold []BrsApiItem `json:"gold"`
}

// BrsApiItem is a single market item from BRS API.
type BrsApiItem struct {
	Symbol string  `json:"symbol"`
	Name   string  `json:"name"`
	Price  float64 `json:"price"`
}

var (
	database       *sql.DB
	pollMu         sync.Mutex
	staleThreshold = 5 * time.Minute
)

func main() {
	port := envOrDefault("PORT", "8080")
	apiKey := os.Getenv("BRS_API_KEY")
	if apiKey == "" {
		log.Fatal("BRS_API_KEY environment variable is required")
	}

	pollSeconds, _ := strconv.Atoi(envOrDefault("POLL_INTERVAL", "60"))
	pollInterval := time.Duration(pollSeconds) * time.Second

	dbPath := envOrDefault("DB_PATH", "/data/gold.db")

	// Initialize SQLite
	var err error
	database, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open SQLite: %v", err)
	}
	defer database.Close()

	// WAL mode for better concurrent reads
	database.Exec("PRAGMA journal_mode=WAL")

	// Create table
	_, err = database.Exec(`
		CREATE TABLE IF NOT EXISTS gold_prices (
			symbol     TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			price_rial INTEGER NOT NULL,
			fetched_at TEXT NOT NULL
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// Initial fetch before starting the HTTP server
	log.Println("[poller] Initial fetch...")
	if err := fetchAndCache(apiKey); err != nil {
		log.Printf("[poller] Initial fetch failed: %v (will retry on next tick)", err)
	}

	// Start background poller
	ticker := time.NewTicker(pollInterval)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ticker.C:
				if err := fetchAndCache(apiKey); err != nil {
					log.Printf("[poller] Fetch failed: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/gold/18k", handleGold18k)
	mux.HandleFunc("GET /health", handleHealth)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		ticker.Stop()
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Gold price service listening on :%s", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func handleGold18k(w http.ResponseWriter, r *http.Request) {
	row := database.QueryRow(
		"SELECT name, price_rial, fetched_at FROM gold_prices WHERE symbol = ?",
		"gold_18k",
	)

	var name string
	var priceRial int64
	var fetchedAtStr string

	if err := row.Scan(&name, &priceRial, &fetchedAtStr); err != nil {
		http.Error(w, `{"error":"no cached price available"}`, http.StatusServiceUnavailable)
		return
	}

	fetchedAt, _ := time.Parse(time.RFC3339, fetchedAtStr)
	stale := time.Since(fetchedAt) > staleThreshold

	resp := GoldPrice{
		Name:      name,
		Price:     priceRial,
		FetchedAt: fetchedAtStr,
		Stale:     stale,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func fetchAndCache(apiKey string) error {
	// Overlap guard
	if !pollMu.TryLock() {
		log.Println("[poller] Previous fetch still in progress, skipping")
		return nil
	}
	defer pollMu.Unlock()

	url := fmt.Sprintf("https://BrsApi.ir/Api/Market/Gold_Currency.php?key=%s", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var apiResp BrsApiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("JSON decode failed: %w", err)
	}

	// Find IR_GOLD_18K
	var gold18k *BrsApiItem
	for i := range apiResp.Gold {
		if apiResp.Gold[i].Symbol == "IR_GOLD_18K" {
			gold18k = &apiResp.Gold[i]
			break
		}
	}

	if gold18k == nil || gold18k.Price == 0 {
		return fmt.Errorf("IR_GOLD_18K not found in API response")
	}

	// Convert Toman to Rial (x10)
	priceRial := int64(gold18k.Price * 10)
	name := gold18k.Name
	if name == "" {
		name = "طلای 18 عیار"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	// Upsert
	_, err = database.Exec(`
		INSERT INTO gold_prices (symbol, name, price_rial, fetched_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(symbol) DO UPDATE SET
			name = excluded.name,
			price_rial = excluded.price_rial,
			fetched_at = excluded.fetched_at
	`, "gold_18k", name, priceRial, now)

	if err != nil {
		return fmt.Errorf("DB upsert failed: %w", err)
	}

	log.Printf("[poller] Updated gold_18k: %s = %d Rial", name, priceRial)
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
