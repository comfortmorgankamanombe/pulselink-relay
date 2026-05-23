package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed dashboard.html
var dashboardHTML string

const baseRPCURL = "https://base-mainnet.g.alchemy.com/v2/aE6JU86iKh_qQRQVfUbmN"

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int    `json:"id"`
}

type rpcResponse struct {
	Result string `json:"result"`
}

func fetchBaseBlockNumber() (uint64, error) {
	body, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: "eth_blockNumber", Params: []any{}, ID: 1})
	resp, err := http.Post(baseRPCURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var rpc rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimPrefix(rpc.Result, "0x"), 16, 64)
}

type BuilderBlock struct {
	BlockNumber string `json:"block_number"`
	BlockHash   string `json:"block_hash"`
	GasFee      string `json:"gas_fee"`
	BuilderID   string `json:"builder_id"`
}

type ProcessedBlock struct {
	BlockNumber string    `json:"block_number"`
	BuilderID   string    `json:"builder_id"`
	Fee         float64   `json:"fee"`
	Time        time.Time `json:"time"`
}

var (
	bestBlock          *BuilderBlock
	totalFeesCollected float64
	blockCount         int
	baseBlockNumber    uint64
	recentBlocks       []ProcessedBlock
	mu                 sync.Mutex
)

func main() {
	fmt.Println("PulseLink Relay starting...")

	// Poll Base mainnet for the latest block every 12 seconds
	go func() {
		poll := func() {
			num, err := fetchBaseBlockNumber()
			if err != nil {
				log.Printf("Base RPC error: %v", err)
				return
			}
			mu.Lock()
			baseBlockNumber = num
			mu.Unlock()
			log.Printf("Base block number: %d", num)
		}
		poll()
		ticker := time.NewTicker(12 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			poll()
		}
	}()

	// Dashboard
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, dashboardHTML)
	})

	// Live stats API consumed by the dashboard
	http.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		recent := make([]ProcessedBlock, len(recentBlocks))
		copy(recent, recentBlocks)
		stats := map[string]any{
			"base_block_number":    baseBlockNumber,
			"blocks_processed":     blockCount,
			"total_fees_collected": totalFeesCollected,
			"recent_blocks":        recent,
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	// Builders ping this to check we're alive
	http.HandleFunc("/relay/v1/builder/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","relay":"pulselink","version":"0.1.0","time":"%s"}`, time.Now().Format(time.RFC3339))
	})

	// Builders POST their blocks here
	http.HandleFunc("/relay/v1/builder/blocks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var block BuilderBlock
		if err := json.NewDecoder(r.Body).Decode(&block); err != nil {
			http.Error(w, "Invalid block data", http.StatusBadRequest)
			return
		}

		if block.BlockHash == "" {
			http.Error(w, "Invalid block: block_hash is required", http.StatusBadRequest)
			return
		}
		if block.BuilderID == "" {
			http.Error(w, "Invalid block: builder_id is required", http.StatusBadRequest)
			return
		}
		fee, err := strconv.ParseFloat(block.GasFee, 64)
		if err != nil || fee <= 0 {
			http.Error(w, "Invalid block: gas_fee must be a number greater than 0", http.StatusBadRequest)
			return
		}

		log.Printf("Block received from builder %s | Block #%s | Fee: %s ETH",
			block.BuilderID, block.BlockNumber, block.GasFee)

		mu.Lock()
		totalFeesCollected += fee
		blockCount++
		bestBlock = &block
		recentBlocks = append([]ProcessedBlock{{
			BlockNumber: block.BlockNumber,
			BuilderID:   block.BuilderID,
			Fee:         fee,
			Time:        time.Now(),
		}}, recentBlocks...)
		if len(recentBlocks) > 10 {
			recentBlocks = recentBlocks[:10]
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"accepted","message":"Block received by PulseLink"}`)
	})

	// Validators call this to get the best block header
	http.HandleFunc("/relay/v1/validator/header", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if bestBlock == nil {
			fmt.Fprintf(w, `{"status":"no_blocks","message":"No blocks received yet"}`)
			return
		}
		fmt.Fprintf(w, `{"status":"ok","best_block":"%s","fee":"%s","builder":"%s"}`,
			bestBlock.BlockHash, bestBlock.GasFee, bestBlock.BuilderID)
	})

	fmt.Println("Listening on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
