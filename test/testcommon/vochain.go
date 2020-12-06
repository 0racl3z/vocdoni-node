package testcommon

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/tendermint/tendermint/privval"
	tmtypes "github.com/tendermint/tendermint/types"
	"google.golang.org/protobuf/proto"

	models "github.com/vocdoni/dvote-protobuf/build/go/models"
	"gitlab.com/vocdoni/go-dvote/config"
	"gitlab.com/vocdoni/go-dvote/crypto/ethereum"
	"gitlab.com/vocdoni/go-dvote/test/testcommon/testutil"
	"gitlab.com/vocdoni/go-dvote/types"
	"gitlab.com/vocdoni/go-dvote/util"
	"gitlab.com/vocdoni/go-dvote/vochain"
	"gitlab.com/vocdoni/go-dvote/vochain/scrutinizer"
)

var (
	SignerPrivKey = "e0aa6db5a833531da4d259fb5df210bae481b276dc4c2ab6ab9771569375aed5"

	OracleListHardcoded = []common.Address{
		common.HexToAddress("0fA7A3FdB5C7C611646a535BDDe669Db64DC03d2"),
		common.HexToAddress("00192Fb10dF37c9FB26829eb2CC623cd1BF599E8"),
		common.HexToAddress("237B54D0163Aa131254fA260Fc12DB0E6DC76FC7"),
		common.HexToAddress("F904848ea36c46817096E94f932A9901E377C8a5"),
		common.HexToAddress("06d0d2c41f4560f8ffea1285f44ce0ffa2e19ef0"),
	}

	// VoteHardcoded needs to be a constructor, since multiple tests will
	// modify its value. We need a different pointer for each test.
	VoteHardcoded = func() *models.Vote {
		vp, _ := base64.StdEncoding.DecodeString("eyJ0eXBlIjoicG9sbC12b3RlIiwibm9uY2UiOiI1NTkyZjFjMThlMmExNTk1M2YzNTVjMzRiMjQ3ZDc1MWRhMzA3MzM4Yzk5NDAwMGI5YTY1ZGIxZGMxNGNjNmMwIiwidm90ZXMiOlsxLDIsMV19")
		return &models.Vote{
			ProcessId:   testutil.Hex2byte(nil, "e9d5e8d791f51179e218c606f83f5967ab272292a6dbda887853d81f7a1d5105"),
			Nullifier:   testutil.Hex2byte(nil, "5592f1c18e2a15953f355c34b247d751da307338c994000b9a65db1dc14cc6c0"), // nullifier and nonce are the same here
			VotePackage: vp,
		}
	}

	ProcessHardcoded = &models.Process{
		ProcessId:    testutil.Hex2byte(nil, "e9d5e8d791f51179e218c606f83f5967ab272292a6dbda887853d81f7a1d5105"),
		EntityId:     testutil.Hex2byte(nil, "180dd5765d9f7ecef810b565a2e5bd14a3ccd536c442b3de74867df552855e85"),
		CensusMkRoot: testutil.Hex2byte(nil, "0a975f5cf517899e6116000fd366dc0feb34a2ea1b64e9b213278442dd9852fe"),
		CensusOrigin: models.CensusOrigin_OFF_CHAIN,
		BlockCount:   1000,
		EnvelopeType: &models.EnvelopeType{},
		Mode:         &models.ProcessMode{},
		Status:       models.ProcessStatus_READY,
	}

	// privKey e0aa6db5a833531da4d259fb5df210bae481b276dc4c2ab6ab9771569375aed5 for address 06d0d2c41f4560f8ffea1285f44ce0ffa2e19ef0
	HardcodedNewProcessTx = &models.NewProcessTx{
		Txtype:  models.TxType_NEW_PROCESS,
		Process: ProcessHardcoded,
		Nonce:   util.RandomBytes(32),
	}

	HardcodedCancelProcessTx = &models.CancelProcessTx{
		Txtype:    models.TxType_CANCEL_PROCESS,
		ProcessId: testutil.Hex2byte(nil, "e9d5e8d791f51179e218c606f83f5967ab272292a6dbda887853d81f7a1d5105"),
		Nonce:     util.RandomBytes(32),
	}

	HardcodedNewVoteTx = &models.VoteEnvelope{
		Nonce:     util.RandomBytes(32),
		Nullifier: testutil.Hex2byte(nil, "5592f1c18e2a15953f355c34b247d751da307338c994000b9a65db1dc14cc6c0"),
		ProcessId: testutil.Hex2byte(nil, "e9d5e8d791f51179e218c606f83f5967ab272292a6dbda887853d81f7a1d5105"),
		Proof: &models.Proof{Payload: &models.Proof_Graviton{Graviton: &models.ProofGraviton{
			Siblings: testutil.Hex2byte(nil, "00030000000000000000000000000000000000000000000000000000000000070ab34471caaefc9bb249cb178335f367988c159f3907530ef7daa1e1bf0c9c7a218f981be7c0c46ffa345d291abb36a17c22722814fb0110240b8640fd1484a6268dc2f0fc2152bf83c06566fbf155f38b8293033d4779a63bba6c7157fd10c8"),
		}}},
		VotePackage: testutil.B642byte(nil, "eyJ0eXBlIjoicG9sbC12b3RlIiwibm9uY2UiOiI1NTkyZjFjMThlMmExNTk1M2YzNTVjMzRiMjQ3ZDc1MWRhMzA3MzM4Yzk5NDAwMGI5YTY1ZGIxZGMxNGNjNmMwIiwidm90ZXMiOlsxLDIsMV19"),
	}

	HardcodedAdminTxAddOracle = &models.AdminTx{
		Txtype:  models.TxType_ADD_ORACLE,
		Address: testutil.Hex2byte(nil, "39106af1fF18bD60a38a296fd81B1f28f315852B"), // oracle address or pubkey validator
		Nonce:   util.RandomBytes(32),
	}

	HardcodedAdminTxRemoveOracle = &models.AdminTx{
		Txtype:  models.TxType_REMOVE_ORACLE,
		Address: testutil.Hex2byte(nil, "00192Fb10dF37c9FB26829eb2CC623cd1BF599E8"),
		Nonce:   util.RandomBytes(32),
	}
	power                        = uint64(10)
	HardcodedAdminTxAddValidator = &models.AdminTx{
		Txtype:  models.TxType_ADD_VALIDATOR,
		Address: testutil.Hex2byte(nil, "5DC922017285EC24415F3E7ECD045665EADA8B5A"),
		Nonce:   util.RandomBytes(32),
		Power:   &power,
	}

	HardcodedAdminTxRemoveValidator = &models.AdminTx{
		Txtype:  models.TxType_REMOVE_VALIDATOR,
		Address: testutil.Hex2byte(nil, "5DC922017285EC24415F3E7ECD045665EADA8B5A"),
		Nonce:   util.RandomBytes(32),
	}
)

func SignAndPrepareTx(vtx *models.Tx) error {
	// Signature
	signer := ethereum.SignKeys{}
	signer.AddHexKey(SignerPrivKey)
	var err error
	txb := []byte{}
	switch vtx.Payload.(type) {
	case *models.Tx_Vote:
		tx := vtx.GetVote()
		txb, err = proto.Marshal(tx)
	case *models.Tx_Admin:
		tx := vtx.GetAdmin()
		txb, err = proto.Marshal(tx)
	case *models.Tx_NewProcess:
		tx := vtx.GetNewProcess()
		txb, err = proto.Marshal(tx)
	case *models.Tx_CancelProcess:
		tx := vtx.GetCancelProcess()
		txb, err = proto.Marshal(tx)
	default:
		err = fmt.Errorf("transaction type unknown")
	}
	if err != nil {
		return err
	}
	hexsign, err := signer.Sign(txb)
	if err != nil {
		return err
	}
	vtx.Signature, err = hex.DecodeString(hexsign)
	return err
}

func NewVochainStateWithOracles(tb testing.TB) *vochain.State {
	s, err := vochain.NewState(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	for _, o := range OracleListHardcoded {
		if err := s.AddOracle(o); err != nil {
			tb.Fatal(err)
		}
	}
	return s
}

func NewVochainStateWithValidators(tb testing.TB) *vochain.State {
	s, err := vochain.NewState(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	vals := make([]*privval.FilePV, 2)
	rint := rand.Int()
	vals[0], err = privval.GenFilePV("/tmp/"+strconv.Itoa(rint), "/tmp/"+strconv.Itoa(rint), tmtypes.ABCIPubKeyTypeEd25519)
	if err != nil {
		tb.Fatal(err)
	}
	rint = rand.Int()
	vals[1], err = privval.GenFilePV("/tmp/"+strconv.Itoa(rint), "/tmp/"+strconv.Itoa(rint), tmtypes.ABCIPubKeyTypeEd25519)
	if err != nil {
		tb.Fatal(err)
	}
	validator0 := &models.Validator{
		Address: vals[0].Key.Address.Bytes(),
		PubKey:  vals[0].Key.PubKey.Bytes(),
		Power:   10,
	}
	validator1 := &models.Validator{
		Address: vals[0].Key.Address.Bytes(),
		PubKey:  vals[0].Key.PubKey.Bytes(),
		Power:   10,
	}
	if err := s.AddValidator(validator0); err != nil {
		tb.Fatal(err)
	}
	if err := s.AddValidator(validator1); err != nil {
		tb.Fatal(err)
	}
	for _, o := range OracleListHardcoded {
		if err := s.AddOracle(o); err != nil {
			tb.Fatal(err)
		}
	}
	return s
}

func NewVochainStateWithProcess(tb testing.TB) *vochain.State {
	s, err := vochain.NewState(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	// add process
	processBytes, err := proto.Marshal(ProcessHardcoded)
	if err != nil {
		tb.Fatal(err)
	}
	if err = s.Store.Tree(vochain.ProcessTree).Add(testutil.Hex2byte(nil, "e9d5e8d791f51179e218c606f83f5967ab272292a6dbda887853d81f7a1d5105"), processBytes); err != nil {
		tb.Fatal(err)
	}
	return s
}

func NewMockVochainNode(tb testing.TB, d *DvoteAPIServer) *vochain.BaseApplication {
	// start vochain node
	// create config
	d.VochainCfg = new(config.VochainCfg)
	d.VochainCfg.DataDir = tb.TempDir()

	// create genesis file
	tmConsensusParams := tmtypes.DefaultConsensusParams()
	consensusParams := &types.ConsensusParams{
		Block: types.BlockParams(tmConsensusParams.Block),
		//Evidence:  types.EvidenceParams(tmConsensusParams.Evidence),
		Validator: types.ValidatorParams(tmConsensusParams.Validator),
	}

	// TO-DO instead of creating a pv file, just create a random 64 bytes key and use it for the genesis file
	validator, err := privval.GenFilePV(d.VochainCfg.DataDir+"/config/priv_validator_key.json", d.VochainCfg.DataDir+"/data/priv_validator_state.json", tmtypes.ABCIPubKeyTypeEd25519)
	if err != nil {
		tb.Fatal(err)
	}
	oracles := []string{d.Signer.AddressString()}
	genBytes, err := vochain.NewGenesis(d.VochainCfg, strconv.Itoa(rand.Int()), consensusParams, []privval.FilePV{*validator}, oracles)
	if err != nil {
		tb.Fatal(err)
	}
	// creating node
	d.VochainCfg.LogLevel = "error"
	d.VochainCfg.P2PListen = "0.0.0.0:26656"
	d.VochainCfg.PublicAddr = "0.0.0.0:26656"
	d.VochainCfg.RPCListen = "0.0.0.0:26657"
	// run node
	d.VochainCfg.MinerKey = fmt.Sprintf("%x", validator.Key.PrivKey)
	vnode := vochain.NewVochain(d.VochainCfg, genBytes)
	tb.Cleanup(func() {
		if err := vnode.Node.Stop(); err != nil {
			tb.Error(err)
		}
		vnode.Node.Wait()
	})
	return vnode
}

func NewMockScrutinizer(tb testing.TB, d *DvoteAPIServer, vnode *vochain.BaseApplication) *scrutinizer.Scrutinizer {
	tb.Log("starting vochain scrutinizer")
	d.ScrutinizerDir = tb.TempDir()
	sc, err := scrutinizer.NewScrutinizer(d.ScrutinizerDir, vnode.State)
	if err != nil {
		tb.Fatal(err)
	}
	return sc
}

// CreateEthRandomKeysBatch creates a set of eth random signing keys
func CreateEthRandomKeysBatch(tb testing.TB, n int) []*ethereum.SignKeys {
	s := make([]*ethereum.SignKeys, n)
	for i := 0; i < n; i++ {
		s[i] = ethereum.NewSignKeys()
		if err := s[i].Generate(); err != nil {
			tb.Fatal(err)
		}
	}
	return s
}
