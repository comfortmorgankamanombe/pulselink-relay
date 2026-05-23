package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed dashboard.html
var dashboardHTML string

const (
	baseRPCURL  = "https://base-mainnet.g.alchemy.com/v2/aE6JU86iKh_qQRQVfUbmN"
	chainID     = 8453
	relayPubkey = "0x000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
	relaySig    = "0x000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
)

// ── Base RPC ──────────────────────────────────────────────────────────────────

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

// ── Relay types ───────────────────────────────────────────────────────────────

type ValidatorRegistrationMessage struct {
	FeeRecipient string `json:"fee_recipient"`
	GasLimit     string `json:"gas_limit"`
	Timestamp    string `json:"timestamp"`
	Pubkey       string `json:"pubkey"`
}

type SignedValidatorRegistration struct {
	Message   ValidatorRegistrationMessage `json:"message"`
	Signature string                       `json:"signature"`
}

type BidTrace struct {
	Slot                 string `json:"slot"`
	ParentHash           string `json:"parent_hash"`
	BlockHash            string `json:"block_hash"`
	BuilderPubkey        string `json:"builder_pubkey"`
	ProposerPubkey       string `json:"proposer_pubkey"`
	ProposerFeeRecipient string `json:"proposer_fee_recipient"`
	GasLimit             string `json:"gas_limit"`
	GasUsed              string `json:"gas_used"`
	Value                string `json:"value"`
}

type ExecutionPayload struct {
	ParentHash    string   `json:"parent_hash"`
	FeeRecipient  string   `json:"fee_recipient"`
	StateRoot     string   `json:"state_root"`
	ReceiptsRoot  string   `json:"receipts_root"`
	LogsBloom     string   `json:"logs_bloom"`
	PrevRandao    string   `json:"prev_randao"`
	BlockNumber   string   `json:"block_number"`
	GasLimit      string   `json:"gas_limit"`
	GasUsed       string   `json:"gas_used"`
	Timestamp     string   `json:"timestamp"`
	ExtraData     string   `json:"extra_data"`
	BaseFeePerGas string   `json:"base_fee_per_gas"`
	BlockHash     string   `json:"block_hash"`
	Transactions  []string `json:"transactions"`
	Withdrawals   []any    `json:"withdrawals,omitempty"`
}

type ExecutionPayloadHeader struct {
	ParentHash       string `json:"parent_hash"`
	FeeRecipient     string `json:"fee_recipient"`
	StateRoot        string `json:"state_root"`
	ReceiptsRoot     string `json:"receipts_root"`
	LogsBloom        string `json:"logs_bloom"`
	PrevRandao       string `json:"prev_randao"`
	BlockNumber      string `json:"block_number"`
	GasLimit         string `json:"gas_limit"`
	GasUsed          string `json:"gas_used"`
	Timestamp        string `json:"timestamp"`
	ExtraData        string `json:"extra_data"`
	BaseFeePerGas    string `json:"base_fee_per_gas"`
	BlockHash        string `json:"block_hash"`
	TransactionsRoot string `json:"transactions_root"`
	WithdrawalsRoot  string `json:"withdrawals_root,omitempty"`
}

// SubmitBlockRequest is what builders POST to /relay/v1/builder/blocks
type SubmitBlockRequest struct {
	Message          BidTrace         `json:"message"`
	ExecutionPayload ExecutionPayload `json:"execution_payload"`
	Signature        string           `json:"signature"`
}

type StoredBlock struct {
	Req        SubmitBlockRequest
	Value      *big.Int
	ReceivedAt time.Time
}

// BuilderBid is returned to proposers requesting headers
type BuilderBid struct {
	Header ExecutionPayloadHeader `json:"header"`
	Value  string                 `json:"value"`
	Pubkey string                 `json:"pubkey"`
}

type SignedBuilderBid struct {
	Message   BuilderBid `json:"message"`
	Signature string     `json:"signature"`
}

type GetHeaderResponse struct {
	Version string           `json:"version"`
	Data    SignedBuilderBid `json:"data"`
}

// SignedBlindedBeaconBlock is what proposers POST to /eth/v1/builder/blinded_blocks
type BlindedBeaconBlockBody struct {
	ExecutionPayloadHeader ExecutionPayloadHeader `json:"execution_payload_header"`
}

type BlindedBeaconBlock struct {
	Slot          string                 `json:"slot"`
	ProposerIndex string                 `json:"proposer_index"`
	ParentRoot    string                 `json:"parent_root"`
	StateRoot     string                 `json:"state_root"`
	Body          BlindedBeaconBlockBody `json:"body"`
}

type SignedBlindedBeaconBlock struct {
	Message   BlindedBeaconBlock `json:"message"`
	Signature string             `json:"signature"`
}

type GetPayloadResponse struct {
	Version string           `json:"version"`
	Data    ExecutionPayload `json:"data"`
}

// ValidatorEntry is one item in GET /relay/v1/builder/validators
type ValidatorEntry struct {
	Slot           string                      `json:"slot"`
	ValidatorIndex string                      `json:"validator_index"`
	Entry          SignedValidatorRegistration `json:"entry"`
}

// ReceivedBidTrace is stored for the data API (all received submissions)
type ReceivedBidTrace struct {
	Slot                 string `json:"slot"`
	ParentHash           string `json:"parent_hash"`
	BlockHash            string `json:"block_hash"`
	BuilderPubkey        string `json:"builder_pubkey"`
	ProposerPubkey       string `json:"proposer_pubkey"`
	ProposerFeeRecipient string `json:"proposer_fee_recipient"`
	GasLimit             string `json:"gas_limit"`
	GasUsed              string `json:"gas_used"`
	Value                string `json:"value"`
	BlockNumber          string `json:"block_number"`
	NumTx                string `json:"num_tx"`
	Timestamp            int64  `json:"timestamp"`
	TimestampMs          int64  `json:"timestamp_ms"`
}

// DeliveredBidTrace is stored when a payload is actually given to a proposer
type DeliveredBidTrace struct {
	Slot                 string `json:"slot"`
	ParentHash           string `json:"parent_hash"`
	BlockHash            string `json:"block_hash"`
	BuilderPubkey        string `json:"builder_pubkey"`
	ProposerPubkey       string `json:"proposer_pubkey"`
	ProposerFeeRecipient string `json:"proposer_fee_recipient"`
	GasLimit             string `json:"gas_limit"`
	GasUsed              string `json:"gas_used"`
	Value                string `json:"value"`
	BlockNumber          string `json:"block_number"`
	NumTx                string `json:"num_tx"`
}

// ProcessedBlock is used by the dashboard /api/stats endpoint
type ProcessedBlock struct {
	BlockNumber string    `json:"block_number"`
	BuilderID   string    `json:"builder_id"`
	Fee         float64   `json:"fee"`
	Time        time.Time `json:"time"`
}

// ── In-memory state ───────────────────────────────────────────────────────────

var (
	// Dashboard stats
	totalFeesCollected float64
	blockCount         int
	baseBlockNumber    uint64
	recentBlocks       []ProcessedBlock

	// Validator registry: pubkey -> registration
	validatorRegs    = make(map[string]SignedValidatorRegistration)
	validatorIdxMap  = make(map[string]int)
	nextValIdx       int

	// Block storage
	bestBlocks   = make(map[string]*StoredBlock) // slot      -> highest-value block
	blocksByHash = make(map[string]*StoredBlock) // blockhash -> block (for payload lookup)

	// Data API logs (newest first, capped at 200)
	receivedBids  []ReceivedBidTrace
	deliveredBids []DeliveredBidTrace

	mu sync.Mutex
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprintf(w, `{"code":%d,"message":%q}`, code, msg)
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func parseWei(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return new(big.Int)
	}
	return v
}

func weiToEth(wei *big.Int) float64 {
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e18)).Float64()
	return f
}

// txsRoot computes a SHA-256 digest of all transaction bytes as a simplified
// transactions root (a full MPT root would require additional libraries).
func txsRoot(txs []string) string {
	h := sha256.New()
	for _, tx := range txs {
		h.Write([]byte(tx))
	}
	return fmt.Sprintf("0x%x", h.Sum(nil))
}

func payloadToHeader(ep ExecutionPayload) ExecutionPayloadHeader {
	return ExecutionPayloadHeader{
		ParentHash:       ep.ParentHash,
		FeeRecipient:     ep.FeeRecipient,
		StateRoot:        ep.StateRoot,
		ReceiptsRoot:     ep.ReceiptsRoot,
		LogsBloom:        ep.LogsBloom,
		PrevRandao:       ep.PrevRandao,
		BlockNumber:      ep.BlockNumber,
		GasLimit:         ep.GasLimit,
		GasUsed:          ep.GasUsed,
		Timestamp:        ep.Timestamp,
		ExtraData:        ep.ExtraData,
		BaseFeePerGas:    ep.BaseFeePerGas,
		BlockHash:        ep.BlockHash,
		TransactionsRoot: txsRoot(ep.Transactions),
	}
}

func queryLimit(r *http.Request, def int) int {
	if s := r.URL.Query().Get("limit"); s != "" {
		if l, err := strconv.Atoi(s); err == nil && l > 0 {
			return l
		}
	}
	return def
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	fmt.Printf("PulseLink Relay starting — Base L2 chain ID %d\n", chainID)

	// Poll Base mainnet block number every 12 seconds
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
			log.Printf("Base block: %d", num)
		}
		poll()
		ticker := time.NewTicker(12 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			poll()
		}
	}()

	// ── Dashboard ─────────────────────────────────────────────────────────────

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, dashboardHTML)
	})

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
		jsonOK(w, stats)
	})

	// ── 1. GET /eth/v1/builder/status ─────────────────────────────────────────
	// Standard MEV-boost health check — mev-boost clients poll this on startup.

	http.HandleFunc("/eth/v1/builder/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Legacy status kept for backwards compatibility
	http.HandleFunc("/relay/v1/builder/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","relay":"pulselink","version":"0.2.0","chain_id":%d,"time":"%s"}`,
			chainID, time.Now().Format(time.RFC3339))
	})

	// ── 2. POST /eth/v1/builder/validators ────────────────────────────────────
	// Validators register their fee_recipient and gas_limit before each epoch.

	http.HandleFunc("/eth/v1/builder/validators", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var regs []SignedValidatorRegistration
		if err := json.NewDecoder(r.Body).Decode(&regs); err != nil {
			jsonErr(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if len(regs) == 0 {
			jsonErr(w, "empty registrations array", http.StatusBadRequest)
			return
		}

		mu.Lock()
		for _, reg := range regs {
			pk := reg.Message.Pubkey
			if pk == "" {
				continue
			}
			if _, exists := validatorIdxMap[pk]; !exists {
				validatorIdxMap[pk] = nextValIdx
				nextValIdx++
			}
			validatorRegs[pk] = reg
		}
		mu.Unlock()

		log.Printf("Validator registration: %d entries stored (total: %d)", len(regs), len(validatorRegs))
		w.WriteHeader(http.StatusOK)
	})

	// ── 3. GET /relay/v1/builder/validators ───────────────────────────────────
	// Builders call this to learn which validators are registered so they know
	// whose blocks to build and what fee_recipient / gas_limit to target.

	http.HandleFunc("/relay/v1/builder/validators", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		mu.Lock()
		slot := strconv.FormatUint(baseBlockNumber, 10)
		entries := make([]ValidatorEntry, 0, len(validatorRegs))
		for pk, reg := range validatorRegs {
			entries = append(entries, ValidatorEntry{
				Slot:           slot,
				ValidatorIndex: strconv.Itoa(validatorIdxMap[pk]),
				Entry:          reg,
			})
		}
		mu.Unlock()

		jsonOK(w, entries)
	})

	// ── 4. POST /relay/v1/builder/blocks ──────────────────────────────────────
	// Builders submit signed block bids. The relay stores the highest-value bid
	// per slot and makes it available to proposers via getHeader.

	http.HandleFunc("/relay/v1/builder/blocks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req SubmitBlockRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Validate required fields
		if req.Message.BlockHash == "" {
			jsonErr(w, "missing block_hash in message", http.StatusBadRequest)
			return
		}
		if req.Message.BuilderPubkey == "" {
			jsonErr(w, "missing builder_pubkey", http.StatusBadRequest)
			return
		}
		if req.ExecutionPayload.BlockHash == "" {
			jsonErr(w, "missing block_hash in execution_payload", http.StatusBadRequest)
			return
		}
		if req.Message.BlockHash != req.ExecutionPayload.BlockHash {
			jsonErr(w, "block_hash mismatch between message and execution_payload", http.StatusBadRequest)
			return
		}

		value := parseWei(req.Message.Value)
		now := time.Now()
		numTx := len(req.ExecutionPayload.Transactions)

		stored := &StoredBlock{Req: req, Value: value, ReceivedAt: now}

		received := ReceivedBidTrace{
			Slot:                 req.Message.Slot,
			ParentHash:           req.Message.ParentHash,
			BlockHash:            req.Message.BlockHash,
			BuilderPubkey:        req.Message.BuilderPubkey,
			ProposerPubkey:       req.Message.ProposerPubkey,
			ProposerFeeRecipient: req.Message.ProposerFeeRecipient,
			GasLimit:             req.Message.GasLimit,
			GasUsed:              req.Message.GasUsed,
			Value:                req.Message.Value,
			BlockNumber:          req.ExecutionPayload.BlockNumber,
			NumTx:                strconv.Itoa(numTx),
			Timestamp:            now.Unix(),
			TimestampMs:          now.UnixMilli(),
		}

		mu.Lock()

		// Keep best block per slot by value
		if existing, ok := bestBlocks[req.Message.Slot]; !ok || value.Cmp(existing.Value) > 0 {
			bestBlocks[req.Message.Slot] = stored
		}
		blocksByHash[req.Message.BlockHash] = stored

		// Data API log (newest first, cap 200)
		receivedBids = append([]ReceivedBidTrace{received}, receivedBids...)
		if len(receivedBids) > 200 {
			receivedBids = receivedBids[:200]
		}

		// Dashboard stats
		blockCount++
		feeEth := weiToEth(value)
		totalFeesCollected += feeEth
		recentBlocks = append([]ProcessedBlock{{
			BlockNumber: req.ExecutionPayload.BlockNumber,
			BuilderID:   req.Message.BuilderPubkey,
			Fee:         feeEth,
			Time:        now,
		}}, recentBlocks...)
		if len(recentBlocks) > 10 {
			recentBlocks = recentBlocks[:10]
		}

		mu.Unlock()

		log.Printf("Block received: slot=%s hash=%s builder=%s value=%s wei (%d txs)",
			req.Message.Slot, req.Message.BlockHash, req.Message.BuilderPubkey, req.Message.Value, numTx)

		jsonOK(w, map[string]string{"status": "accepted"})
	})

	// ── 5. GET /eth/v1/builder/header/{slot}/{parent_hash}/{pubkey} ───────────
	// Proposers call this at the start of their slot to get the best available
	// block header. Returns 204 if no block is available for the slot.

	http.HandleFunc("/eth/v1/builder/header/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/eth/v1/builder/header/"), "/")
		if len(parts) != 3 {
			jsonErr(w, "path must be /eth/v1/builder/header/{slot}/{parent_hash}/{pubkey}", http.StatusBadRequest)
			return
		}
		slot, parentHash := parts[0], parts[1]

		mu.Lock()
		best, ok := bestBlocks[slot]
		mu.Unlock()

		if !ok || best.Req.Message.ParentHash != parentHash {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		jsonOK(w, GetHeaderResponse{
			Version: "capella",
			Data: SignedBuilderBid{
				Message: BuilderBid{
					Header: payloadToHeader(best.Req.ExecutionPayload),
					Value:  best.Req.Message.Value,
					Pubkey: relayPubkey,
				},
				Signature: relaySig,
			},
		})
	})

	// ── 6. POST /eth/v1/builder/blinded_blocks (+ v2) ─────────────────────────
	// Proposer submits their signed blinded block. The relay verifies the block
	// hash, records the delivery, and returns the full execution payload.

	getPayload := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req SignedBlindedBeaconBlock
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, "invalid request body", http.StatusBadRequest)
			return
		}

		blockHash := req.Message.Body.ExecutionPayloadHeader.BlockHash
		if blockHash == "" {
			jsonErr(w, "missing block_hash in execution_payload_header", http.StatusBadRequest)
			return
		}

		mu.Lock()
		stored, ok := blocksByHash[blockHash]
		mu.Unlock()

		if !ok {
			jsonErr(w, "no execution payload found for block_hash "+blockHash, http.StatusBadRequest)
			return
		}

		bid := stored.Req.Message
		numTx := len(stored.Req.ExecutionPayload.Transactions)

		delivered := DeliveredBidTrace{
			Slot:                 bid.Slot,
			ParentHash:           bid.ParentHash,
			BlockHash:            bid.BlockHash,
			BuilderPubkey:        bid.BuilderPubkey,
			ProposerPubkey:       bid.ProposerPubkey,
			ProposerFeeRecipient: bid.ProposerFeeRecipient,
			GasLimit:             bid.GasLimit,
			GasUsed:              bid.GasUsed,
			Value:                bid.Value,
			BlockNumber:          stored.Req.ExecutionPayload.BlockNumber,
			NumTx:                strconv.Itoa(numTx),
		}

		mu.Lock()
		deliveredBids = append([]DeliveredBidTrace{delivered}, deliveredBids...)
		if len(deliveredBids) > 200 {
			deliveredBids = deliveredBids[:200]
		}
		mu.Unlock()

		log.Printf("Payload delivered: slot=%s hash=%s proposer=%s", bid.Slot, blockHash, bid.ProposerPubkey)

		jsonOK(w, GetPayloadResponse{
			Version: "capella",
			Data:    stored.Req.ExecutionPayload,
		})
	}

	http.HandleFunc("/eth/v1/builder/blinded_blocks", getPayload)
	http.HandleFunc("/eth/v2/builder/blinded_blocks", getPayload)

	// ── 7a. GET /relay/v1/data/bidtraces/proposer_payload_delivered ───────────

	http.HandleFunc("/relay/v1/data/bidtraces/proposer_payload_delivered", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		fSlot := q.Get("slot")
		fHash := q.Get("block_hash")
		fBlockNum := q.Get("block_number")
		fProposer := q.Get("proposer_pubkey")
		fBuilder := q.Get("builder_pubkey")
		limit := queryLimit(r, 100)

		mu.Lock()
		all := make([]DeliveredBidTrace, len(deliveredBids))
		copy(all, deliveredBids)
		mu.Unlock()

		out := make([]DeliveredBidTrace, 0, min(limit, len(all)))
		for _, b := range all {
			if fSlot != "" && b.Slot != fSlot { continue }
			if fHash != "" && b.BlockHash != fHash { continue }
			if fBlockNum != "" && b.BlockNumber != fBlockNum { continue }
			if fProposer != "" && b.ProposerPubkey != fProposer { continue }
			if fBuilder != "" && b.BuilderPubkey != fBuilder { continue }
			out = append(out, b)
			if len(out) >= limit { break }
		}
		jsonOK(w, out)
	})

	// ── 7b. GET /relay/v1/data/bidtraces/builder_blocks_received ─────────────

	http.HandleFunc("/relay/v1/data/bidtraces/builder_blocks_received", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		fSlot := q.Get("slot")
		fHash := q.Get("block_hash")
		fBlockNum := q.Get("block_number")
		fBuilder := q.Get("builder_pubkey")
		limit := queryLimit(r, 100)

		mu.Lock()
		all := make([]ReceivedBidTrace, len(receivedBids))
		copy(all, receivedBids)
		mu.Unlock()

		out := make([]ReceivedBidTrace, 0, min(limit, len(all)))
		for _, b := range all {
			if fSlot != "" && b.Slot != fSlot { continue }
			if fHash != "" && b.BlockHash != fHash { continue }
			if fBlockNum != "" && b.BlockNumber != fBlockNum { continue }
			if fBuilder != "" && b.BuilderPubkey != fBuilder { continue }
			out = append(out, b)
			if len(out) >= limit { break }
		}
		jsonOK(w, out)
	})

	// ── 7c. GET /relay/v1/data/validator_registration ────────────────────────

	http.HandleFunc("/relay/v1/data/validator_registration", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		pk := r.URL.Query().Get("pubkey")
		if pk == "" {
			jsonErr(w, "missing pubkey argument", http.StatusBadRequest)
			return
		}
		mu.Lock()
		reg, ok := validatorRegs[pk]
		mu.Unlock()
		if !ok {
			jsonErr(w, "validator not registered", http.StatusBadRequest)
			return
		}
		jsonOK(w, reg)
	})

	// ── Legacy validator header (backwards compat) ────────────────────────────

	http.HandleFunc("/relay/v1/validator/header", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		var best *StoredBlock
		for _, b := range bestBlocks {
			if best == nil || b.Value.Cmp(best.Value) > 0 {
				best = b
			}
		}
		mu.Unlock()
		if best == nil {
			fmt.Fprint(w, `{"status":"no_blocks","message":"No blocks received yet"}`)
			return
		}
		bid := best.Req.Message
		fmt.Fprintf(w, `{"status":"ok","best_block":"%s","value":"%s","builder":"%s"}`,
			bid.BlockHash, bid.Value, bid.BuilderPubkey)
	})

	fmt.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
