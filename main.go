package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// This is what a builder sends us when they submit a block
type BuilderBlock struct {
	BlockNumber string `json:"block_number"`
	BlockHash   string `json:"block_hash"`
	GasFee      string `json:"gas_fee"`
	BuilderID   string `json:"builder_id"`
}

// We store the best block here in memory
var (
	bestBlock          *BuilderBlock
	totalFeesCollected float64
	blockCount         int
	mu                 sync.Mutex
)

func main() {
	fmt.Println("PulseLink Relay starting...")

	// Homepage
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		blocks := blockCount
		fees := totalFeesCollected
		mu.Unlock()
		fmt.Fprintf(w, "PulseLink Relay is live!\nBlocks processed: %d\nTotal ETH collected: %.6f ETH", blocks, fees)
	})

	// Builders ping this to check we're alive
	http.HandleFunc("/relay/v1/builder/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","relay":"pulselink","version":"0.1.0","time":"%s"}`, time.Now().Format(time.RFC3339))
	})

	// THIS IS THE TOLLGATE — builders POST their blocks here
	http.HandleFunc("/relay/v1/builder/blocks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var block BuilderBlock
		err := json.NewDecoder(r.Body).Decode(&block)
		if err != nil {
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

		// Log it so we can see blocks coming in
		log.Printf("Block received from builder %s | Block #%s | Fee: %s ETH",
			block.BuilderID, block.BlockNumber, block.GasFee)

		mu.Lock()
		totalFeesCollected += fee
		blockCount++
		bestBlock = &block
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

