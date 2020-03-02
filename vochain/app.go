package vochain

import (
	"encoding/json"
	"fmt"

	amino "github.com/tendermint/go-amino"
	abcitypes "github.com/tendermint/tendermint/abci/types"
	cryptoAmino "github.com/tendermint/tendermint/crypto/encoding/amino"
	nm "github.com/tendermint/tendermint/node"

	"gitlab.com/vocdoni/go-dvote/log"
	"gitlab.com/vocdoni/go-dvote/types"
	"gitlab.com/vocdoni/go-dvote/util"
)

// BaseApplication reflects the ABCI application implementation.
type BaseApplication struct {
	State *State
	Codec *amino.Codec
	Node  *nm.Node
}

var _ abcitypes.Application = (*BaseApplication)(nil)

// NewBaseApplication creates a new BaseApplication given a name an a DB backend
func NewBaseApplication(dbpath string) (*BaseApplication, error) {
	c := amino.NewCodec()
	cryptoAmino.RegisterAmino(c)
	s, err := NewState(dbpath, c)
	if err != nil {
		return nil, fmt.Errorf("cannot create vochain state: (%s)", err)
	}
	return &BaseApplication{
		State: s,
		Codec: c,
	}, nil
}

// Info Return information about the application state.
// Used to sync Tendermint with the application during a handshake that happens on startup.
// The returned AppVersion will be included in the Header of every block.
// Tendermint expects LastBlockAppHash and LastBlockHeight to be updated during Commit,
// ensuring that Commit is never called twice for the same block height.
func (app *BaseApplication) Info(req abcitypes.RequestInfo) abcitypes.ResponseInfo {
	// print some basic version info about tendermint components (coreVersion, p2pVersion, blockVersion)
	log.Infof("tendermint Core version: %s", req.Version)
	log.Infof("tendermint P2P protocol version: %d", req.P2PVersion)
	log.Infof("tendermint Block protocol version: %d", req.BlockVersion)
	height := app.State.Height()
	hash := app.State.AppHash()
	log.Infof("current height is %d, current APP hash is %x", height, hash)
	return abcitypes.ResponseInfo{
		LastBlockHeight:  height,
		LastBlockAppHash: hash,
	}
}

// InitChain called once upon genesis
// ResponseInitChain can return a list of validators. If the list is empty,
// Tendermint will use the validators loaded in the genesis file.
func (app *BaseApplication) InitChain(req abcitypes.RequestInitChain) abcitypes.ResponseInitChain {
	// setting the app initial state with validators, oracles, height = 0 and empty apphash
	// unmarshal app state from genesis
	var genesisAppState types.GenesisAppState
	err := app.Codec.UnmarshalJSON(req.AppStateBytes, &genesisAppState)
	if err != nil {
		log.Errorf("cannot unmarshal app state bytes: %s", err)
	}
	// get oracles
	for _, v := range genesisAppState.Oracles {
		app.State.AddOracle(v)
	}
	// get validators
	for i := 0; i < len(genesisAppState.Validators); i++ {
		app.State.AddValidator(genesisAppState.Validators[i].PubKey, genesisAppState.Validators[i].Power)
	}

	var header abcitypes.Header
	header.Height = 0
	header.AppHash = []byte{}
	headerBytes, err := app.Codec.MarshalBinaryBare(header)
	if err != nil {
		log.Errorf("cannot marshal header: %s", err)
	}
	app.State.AppTree.Set([]byte(headerKey), headerBytes)
	app.State.Save()

	// TBD: using empty list here, should return validatorsUpdate to use the validators obtained here
	return abcitypes.ResponseInitChain{}
}

// BeginBlock signals the beginning of a new block. Called prior to any DeliverTxs.
// The header contains the height, timestamp, and more - it exactly matches the Tendermint block header.
// The LastCommitInfo and ByzantineValidators can be used to determine rewards and punishments for the validators.
func (app *BaseApplication) BeginBlock(req abcitypes.RequestBeginBlock) abcitypes.ResponseBeginBlock {
	app.State.Lock.Lock()
	// reset app state to latest persistent data
	app.State.Rollback()
	headerBytes, err := app.Codec.MarshalBinaryBare(req.Header)
	if err != nil {
		log.Warnf("cannot marshal header in BeginBlock")
	}
	app.State.AppTree.Set([]byte(headerKey), headerBytes)
	return abcitypes.ResponseBeginBlock{}
}

func (BaseApplication) SetOption(req abcitypes.RequestSetOption) abcitypes.ResponseSetOption {
	return abcitypes.ResponseSetOption{}
}

func (app *BaseApplication) CheckTx(req abcitypes.RequestCheckTx) abcitypes.ResponseCheckTx {
	if _, err := ValidateTx(req.Tx, app.State); err != nil {
		return abcitypes.ResponseCheckTx{Code: 1, Data: []byte(err.Error())}
	}
	return abcitypes.ResponseCheckTx{Code: 0}
}

func (app *BaseApplication) DeliverTx(req abcitypes.RequestDeliverTx) abcitypes.ResponseDeliverTx {
	// ValidateAndDeliverTx should return events
	events, err := ValidateAndDeliverTx(req.Tx, app.State)
	if err != nil {
		return abcitypes.ResponseDeliverTx{Code: 1}
	}
	if events != nil {
		return abcitypes.ResponseDeliverTx{Code: 0, Events: events}
	}
	return abcitypes.ResponseDeliverTx{Code: 0}
}

func (app *BaseApplication) Commit() abcitypes.ResponseCommit {
	defer app.State.Lock.Unlock()
	hash := app.State.Save()
	return abcitypes.ResponseCommit{
		Data: hash,
	}
}

func (app *BaseApplication) Query(req abcitypes.RequestQuery) abcitypes.ResponseQuery {
	var reqData types.QueryData
	err := json.Unmarshal(req.GetData(), &reqData)
	if err != nil {
		return abcitypes.ResponseQuery{Code: 1, Info: fmt.Sprintf("cannot unmarshal request in app query: %s", err)}
	}
	switch reqData.Method {
	case "getEnvelopeStatus":
		_, err := app.State.Envelope(fmt.Sprintf("%s_%s", util.TrimHex(reqData.ProcessID), util.TrimHex(reqData.Nullifier)))
		if err != nil {
			return abcitypes.ResponseQuery{Code: 1}
		}
		return abcitypes.ResponseQuery{Code: 0}
	case "getEnvelope":
		e, err := app.State.Envelope(fmt.Sprintf("%s_%s", util.TrimHex(reqData.ProcessID), util.TrimHex(reqData.Nullifier))) // nullifier hash(addr+pid), processId by pid_nullifier
		if err != nil {
			return abcitypes.ResponseQuery{Code: 1, Info: fmt.Sprintf("cannot get envelope: %s", err)}
		}
		eBytes, err := app.State.Codec.MarshalBinaryBare(e.VotePackage)
		if err != nil {
			return abcitypes.ResponseQuery{Code: 1, Info: "cannot marshal processBytes"}
		}
		return abcitypes.ResponseQuery{Code: 0, Value: eBytes}
	case "getEnvelopeHeight":
		votes := app.State.CountVotes(reqData.ProcessID)
		vBytes, err := app.State.Codec.MarshalBinaryBare(votes)
		if err != nil {
			return abcitypes.ResponseQuery{Code: 1, Info: "cannot marshal votes count bytes"}
		}
		return abcitypes.ResponseQuery{Code: 0, Value: vBytes}
	case "getBlockHeight":
		h := app.State.Height()
		hbytes, err := app.Codec.MarshalBinaryBare(h)
		if err != nil {
			hbytes = []byte{}
		}
		return abcitypes.ResponseQuery{Code: 0, Value: hbytes}
	case "getProcessList":
		return abcitypes.ResponseQuery{Code: 1, Info: "not implemented"}
	case "getEnvelopeList":
		n := app.State.EnvelopeList(reqData.ProcessID, reqData.From, reqData.ListSize)
		if len(n) != 0 {
			nBytes, err := app.State.Codec.MarshalBinaryBare(n)
			if err != nil {
				return abcitypes.ResponseQuery{Code: 1, Info: "cannot marshal envelope list bytes"}
			}
			return abcitypes.ResponseQuery{Code: 0, Value: nBytes, Info: "ok"}
		} else {
			return abcitypes.ResponseQuery{Code: 0, Value: []byte{}, Info: "any envelope available"}
		}
	default:
		return abcitypes.ResponseQuery{Code: 1, Info: "undefined query method"}
	}
}

func (app *BaseApplication) EndBlock(req abcitypes.RequestEndBlock) abcitypes.ResponseEndBlock {
	return abcitypes.ResponseEndBlock{}
}
