package rpc

import (
	"encoding/json"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/indexer"
)

// Handler serves the JSON-RPC methods.
type Handler struct {
	bc      *core.Blockchain
	mempool *core.Mempool
	state   core.State
	indexer *indexer.Indexer
	chainID string
	methods map[string]func(Request) Response
}

func NewHandler(bc *core.Blockchain, mempool *core.Mempool, state core.State, idx *indexer.Indexer, chainID string) *Handler {
	h := &Handler{bc: bc, mempool: mempool, state: state, indexer: idx, chainID: chainID}
	h.methods = map[string]func(Request) Response{
		"getBlockHeight":          func(req Request) Response { return okResponse(req.ID, h.bc.Height()) },
		"getBlock":                h.getBlock,
		"getBalance":              h.getBalance,
		"getAsset":                h.getAsset,
		"getSession":              h.getSession,
		"getListing":              h.getListing,
		"getAssetsByOwner":        h.getAssetsByOwner,
		"getTemplate":             h.getTemplate,
		"getSessionsByPlayer":     h.getSessionsByPlayer,
		"getTxStatus":             h.getTxStatus,
		"getActiveListings":       h.getActiveListings,
		"getInventory":            h.getInventory,
		"getRandomCommitment":     h.getRandomCommitment,
		"sendTx":                  h.sendTx,
		"getMempoolSize":          func(req Request) Response { return okResponse(req.ID, h.mempool.Size()) },
		"getDelegation":           h.getDelegation,
		"getDelegationsByGranter": h.getDelegationsByGranter,
		"getDelegationsByGrantee": h.getDelegationsByGrantee,
	}
	return h
}

func (h *Handler) Dispatch(req Request) Response {
	method, ok := h.methods[req.Method]
	if !ok {
		return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
	return method(req)
}

// decodeParams unmarshals req.Params into dst; on failure it returns the
// error response to send.
func decodeParams(req Request, dst any) (Response, bool) {
	if err := json.Unmarshal(req.Params, dst); err != nil {
		return errResponse(req.ID, CodeInvalidParams, err.Error()), false
	}
	return Response{}, true
}

func missingParam(req Request, msg string) Response {
	return errResponse(req.ID, CodeInvalidParams, msg)
}

// readState runs read under the state read lock so it never sees a block that
// is still being applied.
func (h *Handler) readState(req Request, read func() (any, error)) Response {
	h.state.BlockRLock()
	v, err := read()
	h.state.BlockRUnlock()
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, v)
}

// indexList returns the whole list when neither offset nor limit is given,
// otherwise one page.
func indexList(req Request, offset, limit int, all func() ([]string, error), page func(offset, limit int) (*indexer.PaginatedResult, error)) Response {
	var (
		result any
		err    error
	)
	if limit == 0 && offset == 0 {
		result, err = all()
	} else {
		result, err = page(offset, limit)
	}
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, result)
}

// getBlock looks up by hash, else by height, else returns the tip; params are
// optional.
func (h *Handler) getBlock(req Request) Response {
	var params struct {
		Hash   string `json:"hash"`
		Height *int64 `json:"height"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResponse(req.ID, CodeInvalidParams, "params: "+err.Error())
		}
	}

	var block *core.Block
	var err error
	switch {
	case params.Hash != "":
		block, err = h.bc.GetBlock(params.Hash)
	case params.Height != nil:
		block, err = h.bc.GetBlockByHeight(*params.Height)
	default:
		block = h.bc.Tip()
	}
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	if block == nil {
		return okResponse(req.ID, nil)
	}
	return okResponse(req.ID, block)
}

func (h *Handler) getBalance(req Request) Response {
	var params struct {
		Address string `json:"address"`
	}
	if resp, ok := decodeParams(req, &params); !ok {
		return resp
	}
	if params.Address == "" {
		return missingParam(req, "address is required")
	}
	return h.readState(req, func() (any, error) {
		acc, err := h.state.GetAccount(params.Address)
		if err != nil {
			return nil, err
		}
		return map[string]any{"address": params.Address, "balance": acc.Balance, "nonce": acc.Nonce}, nil
	})
}

// getByID serves the methods that read one state record by its "id" param.
func (h *Handler) getByID(req Request, get func(id string) (any, error)) Response {
	var params struct {
		ID string `json:"id"`
	}
	if resp, ok := decodeParams(req, &params); !ok {
		return resp
	}
	if params.ID == "" {
		return missingParam(req, "id is required")
	}
	return h.readState(req, func() (any, error) { return get(params.ID) })
}

func (h *Handler) getAsset(req Request) Response {
	return h.getByID(req, func(id string) (any, error) { return h.state.GetAsset(id) })
}

func (h *Handler) getSession(req Request) Response {
	return h.getByID(req, func(id string) (any, error) { return h.state.GetSession(id) })
}

func (h *Handler) getListing(req Request) Response {
	return h.getByID(req, func(id string) (any, error) { return h.state.GetListing(id) })
}

func (h *Handler) getTemplate(req Request) Response {
	return h.getByID(req, func(id string) (any, error) { return h.state.GetTemplate(id) })
}

func (h *Handler) getRandomCommitment(req Request) Response {
	return h.getByID(req, func(id string) (any, error) { return h.state.GetRandomCommitment(id) })
}

func (h *Handler) getInventory(req Request) Response {
	var params struct {
		Owner string `json:"owner"`
	}
	if resp, ok := decodeParams(req, &params); !ok {
		return resp
	}
	if params.Owner == "" {
		return missingParam(req, "owner is required")
	}
	return h.readState(req, func() (any, error) { return h.state.GetInventory(params.Owner) })
}

func (h *Handler) getAssetsByOwner(req Request) Response {
	var params struct {
		Owner  string `json:"owner"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if resp, ok := decodeParams(req, &params); !ok {
		return resp
	}
	if params.Owner == "" {
		return missingParam(req, "owner is required")
	}
	return indexList(req, params.Offset, params.Limit,
		func() ([]string, error) { return h.indexer.GetAssetsByOwner(params.Owner) },
		func(o, l int) (*indexer.PaginatedResult, error) {
			return h.indexer.GetAssetsByOwnerPaginated(params.Owner, o, l)
		})
}

func (h *Handler) getSessionsByPlayer(req Request) Response {
	var params struct {
		Player string `json:"player"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if resp, ok := decodeParams(req, &params); !ok {
		return resp
	}
	if params.Player == "" {
		return missingParam(req, "player is required")
	}
	return indexList(req, params.Offset, params.Limit,
		func() ([]string, error) { return h.indexer.GetSessionsByPlayer(params.Player) },
		func(o, l int) (*indexer.PaginatedResult, error) {
			return h.indexer.GetSessionsByPlayerPaginated(params.Player, o, l)
		})
}

// getActiveListings ignores malformed params and treats them as absent.
func (h *Handler) getActiveListings(req Request) Response {
	var params struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	return indexList(req, params.Offset, params.Limit, h.indexer.GetActiveListings, h.indexer.GetActiveListingsPaginated)
}

func (h *Handler) getTxStatus(req Request) Response {
	var params struct {
		TxID string `json:"tx_id"`
	}
	if resp, ok := decodeParams(req, &params); !ok {
		return resp
	}
	if params.TxID == "" {
		return missingParam(req, "tx_id is required")
	}
	result, err := h.indexer.GetTxResult(params.TxID)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, result)
}

func (h *Handler) sendTx(req Request) Response {
	var tx core.Transaction
	if resp, ok := decodeParams(req, &tx); !ok {
		return resp
	}
	if tx.ChainID != h.chainID {
		return errResponse(req.ID, CodeInvalidParams,
			fmt.Sprintf("chain ID mismatch: got %q want %q", tx.ChainID, h.chainID))
	}
	tx.ID = tx.Hash()
	if err := h.mempool.Add(&tx); err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, map[string]string{"tx_id": tx.ID})
}

func (h *Handler) getDelegation(req Request) Response {
	var params struct {
		Granter string `json:"granter"`
		Grantee string `json:"grantee"`
	}
	if resp, ok := decodeParams(req, &params); !ok {
		return resp
	}
	if params.Granter == "" || params.Grantee == "" {
		return missingParam(req, "granter and grantee are required")
	}
	return h.readState(req, func() (any, error) { return h.state.GetDelegation(params.Granter, params.Grantee) })
}

func (h *Handler) getDelegationsByGranter(req Request) Response {
	var params struct {
		Granter string `json:"granter"`
	}
	if resp, ok := decodeParams(req, &params); !ok {
		return resp
	}
	if params.Granter == "" {
		return missingParam(req, "granter is required")
	}
	return h.readState(req, func() (any, error) { return h.state.GetDelegationsByGranter(params.Granter) })
}

func (h *Handler) getDelegationsByGrantee(req Request) Response {
	var params struct {
		Grantee string `json:"grantee"`
	}
	if resp, ok := decodeParams(req, &params); !ok {
		return resp
	}
	if params.Grantee == "" {
		return missingParam(req, "grantee is required")
	}
	return h.readState(req, func() (any, error) { return h.state.GetDelegationsByGrantee(params.Grantee) })
}
