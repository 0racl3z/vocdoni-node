package main

import (
	"os"

	flag "github.com/spf13/pflag"

	"gitlab.com/vocdoni/go-dvote/crypto/signature"
	"gitlab.com/vocdoni/go-dvote/data"
	"gitlab.com/vocdoni/go-dvote/ipfssync"
	"gitlab.com/vocdoni/go-dvote/log"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	userDir := home + "/.ipfs"
	logLevel := flag.String("logLevel", "info", "log level")
	dataDir := flag.String("dataDir", userDir, "directory for storing data")
	key := flag.String("key", "", "symetric key of the sync ipfs cluster")
	port := flag.Int16("port", 4171, "port for the sync network")
	helloTime := flag.Int("helloTime", 40, "period in seconds for sending hello messages")
	updateTime := flag.Int("updateTime", 20, "period in seconds for sending update messages")
	flag.Parse()
	log.InitLogger(*logLevel, "stdout")
	ipfsStore := data.IPFSNewConfig(*dataDir)
	storage, err := data.Init(data.StorageIDFromString("IPFS"), ipfsStore)
	if err != nil {
		log.Fatal(err)
	}
	var sk signature.SignKeys
	sk.Generate()
	_, privKey := sk.HexString()
	is := ipfssync.NewIPFSsync(*dataDir, *key, privKey, storage)
	is.HelloTime = *helloTime
	is.UpdateTime = *updateTime
	is.Port = *port
	is.Start()

	// Block forever.
	select {}
}
