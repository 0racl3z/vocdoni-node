package types

import (
	"github.com/tendermint/tendermint/crypto"
	tmtypes "github.com/tendermint/tendermint/types"
)

// ________________________ STATE ________________________
// Defined in ../../db/iavl.go for convenience

// ________________________ VOTE ________________________

// VotePackageStruct represents a vote package
type VotePackageStruct struct {
	// Nonce vote nonce
	Nonce string `json:"nonce"`
	// Type vote type
	Type string `json:"type"`
	// Votes directly mapped to the `questions` field of the process metadata
	Votes []int `json:"votes"`
}

// Vote represents a signle Vote
type Vote struct {
	// Nonce unique number per vote attempt, so that replay attacks can't reuse this payload
	Nonce string `json:"nonce,omitempty"`
	// Nullifier is the hash of the private key
	Nullifier string `json:"nullifier,omitempty"`
	// ProcessID contains the vote itself
	ProcessID string `json:"processId,omitempty"`
	// Proof contains the prove indicating that the user is in the census of the process
	Proof string `json:"proof,omitempty"`
	// Signature sign( JSON.stringify( { nonce, processId, proof, 'votePackage' } ), privateKey )
	Signature string `json:"signature,omitempty"`
	// VotePackage base64 encoded vote content
	VotePackage string `json:"votePackage,omitempty"`
}

// ________________________ PROCESS ________________________

// Process represents a state per process
type Process struct {
	// Canceled if true process is canceled
	Canceled bool `json:"canceled,omitempty"`
	// Paused if true process is paused and cannot add or modify any vote
	Paused bool `json:"paused,omitempty"`
	// EncryptionPublicKey are the keys required to encrypt the votes
	EncryptionPublicKeys []string `json:"encryptionPublicKeys,omitempty"`
	// EntityID identifies unequivocally a process
	EntityID string `json:"entityId,omitempty"`
	// MkRoot merkle root of all the census in the process
	MkRoot string `json:"mkRoot,omitempty"`
	// NumberOfBlocks represents the amount of tendermint blocks that the process will last
	NumberOfBlocks int64 `json:"numberOfBlocks,omitempty"`
	// StartBlock represents the tendermint block where the process goes from scheduled to active
	StartBlock int64 `json:"startBlock,omitempty"`
	// Type represents the process type
	Type string `json:"type,omitempty"`
}

// ________________________ TX ________________________

// ValidTypes represents an allowed specific tx type
var ValidTypes = map[string]string{
	"vote":            "VoteTx",
	"newProcess":      "NewProcessTx",
	"cancelProcess":   "CancelProcessTx",
	"addValidator":    "AdminTx",
	"removeValidator": "AdminTx",
	"addOracle":       "AdminTx",
	"removeOracle":    "AdminTx",
}

// Tx is an abstraction for any specific tx which is primarly defined by its type
// For now we have 3 tx types {voteTx, newProcessTx, adminTx}
type Tx struct {
	Type string `json:"type"`
}

// VoteTx represents the info required for submmiting a vote
type VoteTx struct {
	Nonce       string `json:"nonce,omitempty"`
	Nullifier   string `json:"nullifier,omitempty"`
	ProcessID   string `json:"processId"`
	Proof       string `json:"proof,omitempty"`
	Signature   string `json:"signature,omitempty"`
	Type        string `json:"type,omitempty"`
	VotePackage string `json:"votePackage,omitempty"`
}

// NewProcessTx represents the info required for starting a new process
type NewProcessTx struct {
	// EncryptionPublicKeys are the keys required to encrypt the votes
	EncryptionPublicKeys []string `json:"encryptionPublicKeys,omitempty"`
	// EntityID the process belongs to
	EntityID string `json:"entityId"`
	// MkRoot merkle root of all the census in the process
	MkRoot string `json:"mkRoot,omitempty"`
	// MkURI merkle tree URI
	MkURI string `json:"mkURI,omitempty"`
	// NumberOfBlocks represents the tendermint block where the process goes from active to finished
	NumberOfBlocks int64  `json:"numberOfBlocks"`
	ProcessID      string `json:"processId"`
	ProcessType    string `json:"processType"`
	Signature      string `json:"signature,omitempty"`
	// StartBlock represents the tendermint block where the process goes from scheduled to active
	StartBlock int64  `json:"startBlock"`
	Type       string `json:"type,omitempty"`
}

// CancelProcessTx represents a tx for canceling a valid process
type CancelProcessTx struct {
	// EntityID the process belongs to
	ProcessID string `json:"processId"`
	Signature string `json:"signature,omitempty"`
	Type      string `json:"type,omitempty"`
}

// AdminTx represents a Tx that can be only executed by some authorized addresses
type AdminTx struct {
	Address   string        `json:"address"`
	Nonce     string        `json:"nonce"`
	Power     int64         `json:"power,omitempty"`
	PubKey    crypto.PubKey `json:"pub_key,omitempty"`
	Signature string        `json:"signature,omitempty"`
	Type      string        `json:"type"` // addValidator, removeValidator, addOracle, removeOracle
}

// ValidateType a valid Tx type specified in ValidTypes
func ValidateType(t string) string {
	val, ok := ValidTypes[t]
	if !ok {
		return ""
	}
	return val
}

// ________________________ VALIDATORS ________________________

// ________________________ QUERIES ________________________

// QueryData is an abstraction of any kind of data a query request could have
type QueryData struct {
	Method      string `json:"method"`
	ProcessID   string `json:"processId,omitempty"`
	Nullifier   string `json:"nullifier,omitempty"`
	From        int64  `json:"from,omitempty"`
	ListSize    int64  `json:"listSize,omitempty"`
	Timestamp   int64  `json:"timestamp,omitempty"`
	ProcessType string `json:"type,omitempty"`
}

// ________________________ GENESIS APP STATE ________________________

// GenesisAppState application state in genesis
type GenesisAppState struct {
	Validators []tmtypes.GenesisValidator `json:"validators"`
	Oracles    []string                   `json:"oracles"`
}

// ________________________ CALLBACKS DATA STRUCTS ________________________

// ScrutinizerOnProcessData holds the required data for callbacks when
// a new process is added into the vochain.
type ScrutinizerOnProcessData struct {
	EntityID  string
	ProcessID string
}
