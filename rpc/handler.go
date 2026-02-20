package rpc

import (
	"encoding/json"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/indexer"
)

// Handler holds all dependencies needed to serve RPC methods.
type Handler struct {
	bc      *core.Blockchain
	mempool *core.Mempool
	state   core.State
	indexer *indexer.Indexer
	chainID string // expected chain_id; used to reject cross-chain replay transactions
}

// NewHandler creates an RPC Handler.
func NewHandler(bc *core.Blockchain, mempool *core.Mempool, state core.State, idx *indexer.Indexer, chainID string) *Handler {
	return &Handler{bc: bc, mempool: mempool, state: state, indexer: idx, chainID: chainID}
}

// Dispatch routes an RPC request to the correct method.
func (h *Handler) Dispatch(req Request) Response {
	switch req.Method {
	case "getBlockHeight":
		return okResponse(req.ID, h.bc.Height())

	case "getBlock":
		return h.getBlock(req)

	case "getBalance":
		return h.getBalance(req)

	case "getAsset":
		return h.getAsset(req)

	case "getSession":
		return h.getSession(req)

	case "getListing":
		return h.getListing(req)

	case "getAssetsByOwner":
		return h.getAssetsByOwner(req)

	case "sendTx":
		return h.sendTx(req)

	case "getMempoolSize":
		return okResponse(req.ID, h.mempool.Size())

	default:
		return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

func (h *Handler) getBlock(req Request) Response {
	var params struct {
		Hash   string `json:"hash"`
		Height *int64 `json:"height"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeInvalidParams, "params: "+err.Error())
	}

	var block *core.Block
	var err error
	if params.Hash != "" {
		block, err = h.bc.GetBlock(params.Hash)
	} else if params.Height != nil {
		block, err = h.bc.GetBlockByHeight(*params.Height)
	} else {
		block = h.bc.Tip()
	}
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	if block == nil {
		return errResponse(req.ID, CodeInternalError, "no block found")
	}
	return okResponse(req.ID, block)
}

func (h *Handler) getBalance(req Request) Response {
	var params struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeInvalidParams, err.Error())
	}
	if params.Address == "" {
		return errResponse(req.ID, CodeInvalidParams, "address is required")
	}
	acc, err := h.state.GetAccount(params.Address)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, map[string]any{"address": params.Address, "balance": acc.Balance, "nonce": acc.Nonce})
}

func (h *Handler) getAsset(req Request) Response {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeInvalidParams, err.Error())
	}
	if params.ID == "" {
		return errResponse(req.ID, CodeInvalidParams, "id is required")
	}
	asset, err := h.state.GetAsset(params.ID)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, asset)
}

func (h *Handler) getSession(req Request) Response {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeInvalidParams, err.Error())
	}
	if params.ID == "" {
		return errResponse(req.ID, CodeInvalidParams, "id is required")
	}
	sess, err := h.state.GetSession(params.ID)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, sess)
}

func (h *Handler) getListing(req Request) Response {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeInvalidParams, err.Error())
	}
	if params.ID == "" {
		return errResponse(req.ID, CodeInvalidParams, "id is required")
	}
	listing, err := h.state.GetListing(params.ID)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, listing)
}

func (h *Handler) getAssetsByOwner(req Request) Response {
	var params struct {
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeInvalidParams, err.Error())
	}
	if params.Owner == "" {
		return errResponse(req.ID, CodeInvalidParams, "owner is required")
	}
	ids, err := h.indexer.GetAssetsByOwner(params.Owner)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, ids)
}

func (h *Handler) sendTx(req Request) Response {
	var tx core.Transaction
	if err := json.Unmarshal(req.Params, &tx); err != nil {
		return errResponse(req.ID, CodeInvalidParams, err.Error())
	}
	// Reject transactions destined for a different network to prevent
	// cross-chain replay attacks.
	if tx.ChainID != h.chainID {
		return errResponse(req.ID, CodeInvalidParams,
			fmt.Sprintf("chain ID mismatch: got %q want %q", tx.ChainID, h.chainID))
	}
	// Recompute the ID server-side; do not trust the client-provided value.
	tx.ID = tx.Hash()
	if err := h.mempool.Add(&tx); err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, map[string]string{"tx_id": tx.ID})
}
