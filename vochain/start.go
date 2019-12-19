// Package vochain provides all the functions for creating and managing a vocdoni voting blockchain
package vochain

import (
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"

	"gitlab.com/vocdoni/go-dvote/config"

	cfg "github.com/tendermint/tendermint/config"
	tmflags "github.com/tendermint/tendermint/libs/cli/flags"
	cmn "github.com/tendermint/tendermint/libs/common"
	tlog "github.com/tendermint/tendermint/libs/log"
	nm "github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	tmtypes "github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"

	"gitlab.com/vocdoni/go-dvote/log"
)

// testing purposes until genesis
const testOracleAddress = "0xF904848ea36c46817096E94f932A9901E377C8a5"

// DefaultSeedNodes list of default Vocdoni seed nodes
var DefaultSeedNodes = []string{"121e65eb5994874d9c05cd8d584a54669d23f294@116.202.8.150:11714"}

// Start starts a new vochain validator node
func NewVochain(globalCfg config.VochainCfg) *BaseApplication {
	// create application db
	log.Info("initializing Vochain")
	// creating new vochain app
	app, err := NewBaseApplication(globalCfg.DataDir + "/data")
	if err != nil {
		log.Errorf("cannot init vochain application: %s", err)
	}

	log.Info("creating node and application")
	go func() {
		app.Node, err = newTendermint(app, globalCfg)
		if err != nil {
			log.Fatal(err)
		}
		err = app.Node.Start()
		if err != nil {
			log.Fatal(err)
		}
	}()
	return app
}

func Start(node *nm.Node) error {
	return node.Start()
}

// NewGenesis creates a new genesis file and saves it to tconfig.Genesis path
func NewGenesis(tconfig *cfg.Config, pv *privval.FilePV) error {
	log.Info("creating genesis file")
	consensusParams := tmtypes.DefaultConsensusParams()
	consensusParams.Block.TimeIotaMs = 20000

	genDoc := tmtypes.GenesisDoc{
		ChainID:         "0x2",
		GenesisTime:     tmtime.Now(),
		ConsensusParams: consensusParams,
	}

	list := make([]tmtypes.GenesisValidator, 0)
	list = append(list, tmtypes.GenesisValidator{
		Address: pv.GetPubKey().Address(),
		PubKey:  pv.GetPubKey(),
		Power:   10,
	})

	// set validators from eth smart contract
	genDoc.Validators = list
	// save genesis
	if err := genDoc.SaveAs(tconfig.GenesisFile()); err != nil {
		return err
	}
	log.Infof("genesis file: %+v", tconfig.Genesis)
	return nil
}

// tenderLogger implements tendermint's Logger interface, with a couple of
// modifications.
//
// First, it routes the logs to go-dvote's logger, so that we don't end up with
// two loggers writing directly to stdout or stderr.
//
// Second, because we generally don't care about tendermint errors such as
// failures to connect to peers, we route all log levels to our debug level.
// They will only surface if dvote's log level is "debug".
type tenderLogger struct {
	keyvals []interface{}
}

var _ tlog.Logger = (*tenderLogger)(nil)

// TODO(demogat): use zap's WithCallerSkip so that we show the position
// information corresponding to where tenderLogger was called, instead of just
// the pointless positions here.

func (l *tenderLogger) Debug(msg string, keyvals ...interface{}) {
	log.Debugw("[tendermint debug] "+msg, keyvals...)
}

func (l *tenderLogger) Info(msg string, keyvals ...interface{}) {
	log.Debugw("[tendermint info] "+msg, keyvals...)
}

func (l *tenderLogger) Error(msg string, keyvals ...interface{}) {
	log.Debugw("[tendermint error] "+msg, keyvals...)
}

func (l *tenderLogger) With(keyvals ...interface{}) tlog.Logger {
	// Make sure we copy the values, to avoid modifying the parent.
	// TODO(demogat): use zap's With method directly.
	l2 := &tenderLogger{}
	l2.keyvals = append(l2.keyvals, l.keyvals...)
	l2.keyvals = append(l2.keyvals, keyvals...)
	return l2
}

// we need to set init (first time validators and oracles)
func newTendermint(app *BaseApplication, localConfig config.VochainCfg) (*nm.Node, error) {
	// create node config
	var err error

	tconfig := cfg.DefaultConfig()
	tconfig.FastSyncMode = true
	tconfig.SetRoot(localConfig.DataDir)
	os.MkdirAll(localConfig.DataDir+"/config", 0755)
	os.MkdirAll(localConfig.DataDir+"/data", 0755)

	tconfig.LogLevel = localConfig.LogLevel
	tconfig.RPC.ListenAddress = "tcp://" + localConfig.RpcListen
	tconfig.P2P.ListenAddress = localConfig.P2pListen
	tconfig.P2P.ExternalAddress = localConfig.PublicAddr
	log.Infof("announcing external address %s", tconfig.P2P.ExternalAddress)

	if !localConfig.CreateGenesis {
		if len(localConfig.Seeds) == 0 && !localConfig.SeedMode {
			tconfig.P2P.Seeds = strings.Join(DefaultSeedNodes[:], ",")
		} else {
			tconfig.P2P.Seeds = strings.Trim(strings.Join(localConfig.Seeds[:], ","), "[]")
		}
		log.Infof("seed nodes: %s", tconfig.P2P.Seeds)

		if len(localConfig.Peers) > 0 {
			tconfig.P2P.PersistentPeers = strings.Trim(strings.Join(localConfig.Peers[:], ","), "[]")
		}
		log.Infof("persistent peers: %s", tconfig.P2P.PersistentPeers)
	}

	tconfig.P2P.AddrBookStrict = false
	tconfig.P2P.SeedMode = localConfig.SeedMode
	tconfig.RPC.CORSAllowedOrigins = []string{"*"}
	tconfig.Consensus.TimeoutPropose = time.Second * 3
	tconfig.Consensus.TimeoutPrevote = time.Second * 1
	tconfig.Consensus.TimeoutPrecommit = time.Second * 1
	tconfig.Consensus.TimeoutCommit = time.Second * 1

	if localConfig.Genesis != "" && !localConfig.CreateGenesis {
		if isAbs := strings.HasPrefix(localConfig.Genesis, "/"); !isAbs {
			dir, err := os.Getwd()
			if err != nil {
				log.Fatal(err)
			}
			tconfig.Genesis = dir + "/" + localConfig.Genesis

		} else {
			tconfig.Genesis = localConfig.Genesis
		}
	} else {
		tconfig.Genesis = tconfig.GenesisFile()
	}

	if err := tconfig.ValidateBasic(); err != nil {
		return nil, errors.Wrap(err, "config is invalid")
	}

	// create logger
	logger := tlog.Logger(&tenderLogger{})

	logger, err = tmflags.ParseLogLevel(tconfig.LogLevel, logger, cfg.DefaultLogLevel())
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse log level")
	}

	// read or create private validator
	var minerKeyFile string
	if localConfig.MinerKeyFile == "" {
		minerKeyFile = tconfig.PrivValidatorKeyFile()
	} else {
		if isAbs := strings.HasPrefix(localConfig.MinerKeyFile, "/"); !isAbs {
			dir, err := os.Getwd()
			if err != nil {
				log.Fatal(err)
			}
			minerKeyFile = dir + "/" + localConfig.MinerKeyFile
		} else {
			minerKeyFile = localConfig.MinerKeyFile
		}
		if !cmn.FileExists(tconfig.PrivValidatorKeyFile()) {
			filePV := privval.LoadFilePVEmptyState(minerKeyFile, tconfig.PrivValidatorStateFile())
			filePV.Save()
		}
	}

	log.Infof("using miner key file %s", minerKeyFile)
	pv := privval.LoadOrGenFilePV(
		minerKeyFile,
		tconfig.PrivValidatorStateFile(),
	)

	// read or create node key
	var nodeKey *p2p.NodeKey
	if localConfig.KeyFile != "" {
		nodeKey, err = p2p.LoadOrGenNodeKey(localConfig.KeyFile)
		log.Infof("using keyfile %s", localConfig.KeyFile)
	} else {
		nodeKey, err = p2p.LoadOrGenNodeKey(tconfig.NodeKeyFile())
		log.Infof("using keyfile %s", tconfig.NodeKeyFile())
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to load node's key")
	}
	log.Infof("my vochain address: %s", nodeKey.PubKey().Address())
	log.Infof("my vochain ID: %s", nodeKey.ID())

	// read or create genesis file
	if cmn.FileExists(tconfig.Genesis) {
		log.Infof("found genesis file %s", tconfig.Genesis)
	} else {
		log.Infof("loaded genesis: %s", TestnetGenesis1)
		err := ioutil.WriteFile(tconfig.Genesis, []byte(TestnetGenesis1), 0644)
		if err != nil {
			log.Warn(err)
		} else {
			log.Infof("new testnet genesis created, stored at %s", tconfig.Genesis)
		}
	}

	// create node
	node, err := nm.NewNode(
		tconfig,
		pv,      // the node val
		nodeKey, // node val key
		proxy.NewLocalClientCreator(app),
		// Note we use proxy.NewLocalClientCreator here to create a local client instead of one communicating through a socket or gRPC.
		nm.DefaultGenesisDocProviderFunc(tconfig),
		nm.DefaultDBProvider,
		nm.DefaultMetricsProvider(tconfig.Instrumentation),
		logger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new Tendermint node")
	}
	log.Debugf("consensus config %+v", *node.Config().Consensus)

	return node, nil
}
