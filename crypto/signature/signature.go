// Package signature provides the cryptographic operations used in go-dvote
package signature

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/ethereum/go-ethereum/crypto"
	i3utils "github.com/iden3/go-iden3-core/merkletree"
	"github.com/iden3/go-iden3-crypto/poseidon"

	"gitlab.com/vocdoni/go-dvote/util"
)

// AddressLength is the lenght of an Ethereum address
const AddressLength = 20

// SignatureLength is the size of an ECDSA signature in hexString format
const SignatureLength = 130

// PubKeyLength is the size of a Public Key
const PubKeyLength = 66

// PubKeyCompLength is the size of a uncompressed Public Key
const PubKeyLengthUncompressed = 130

// SigningPrefix is the prefix added when hashing
const SigningPrefix = "\u0019Ethereum Signed Message:\n"

// SignKeys represents an ECDSA pair of keys for signing.
// Authorized addresses is a list of Ethereum like addresses which are checked on Verify
type SignKeys struct {
	Public     ecdsa.PublicKey
	Private    ecdsa.PrivateKey
	Authorized []Address
}

// Address is an Ethereum like address
type Address [AddressLength]byte

// addrFromString decodes an address from a hex string.
func addrFromString(s string) (Address, error) {
	s = util.TrimHex(s)
	addrBytes, err := hex.DecodeString(s)
	if err != nil {
		return Address{}, err
	}
	if len(addrBytes) != AddressLength {
		return Address{}, fmt.Errorf("invalid address length")
	}
	var addr Address
	copy(addr[:], addrBytes)
	return addr, nil
}

// Generate generates new keys
func (k *SignKeys) Generate() error {
	key, err := crypto.GenerateKey()
	if err != nil {
		return err
	}
	k.Private = *key
	k.Public = key.PublicKey
	return nil
}

// AddHexKey imports a private hex key
func (k *SignKeys) AddHexKey(privHex string) error {
	key, err := crypto.HexToECDSA(util.TrimHex(privHex))
	if err != nil {
		return err
	}
	k.Private = *key
	k.Public = key.PublicKey
	return nil
}

// AddAuthKey adds a new authorized address key
func (k *SignKeys) AddAuthKey(address string) error {
	addr, err := addrFromString(address)
	if err != nil {
		return err
	}
	k.Authorized = append(k.Authorized, addr)
	return nil
}

// HexString returns the public compressed and private keys as hex strings
func (k *SignKeys) HexString() (string, string) {
	pubHexComp := fmt.Sprintf("%x", crypto.CompressPubkey(&k.Public))
	privHex := fmt.Sprintf("%x", crypto.FromECDSA(&k.Private))
	return pubHexComp, privHex
}

// decompressPubKey takes a hexString compressed public key and returns it descompressed
func decompressPubKey(pubHexComp string) (string, error) {
	if len(pubHexComp) > PubKeyLength {
		return pubHexComp, nil
	}
	pubBytes, err := hex.DecodeString(pubHexComp)
	if err != nil {
		return "", err
	}
	pub, err := crypto.DecompressPubkey(pubBytes)
	if err != nil {
		return "", err
	}
	pubHex := fmt.Sprintf("%x", crypto.FromECDSAPub(pub))
	return pubHex, nil
}

// PublicKey return the Ethereum address from the ECDSA public key
func (k *SignKeys) EthAddrString() string {
	recoveredAddr := crypto.PubkeyToAddress(k.Public)
	return fmt.Sprintf("%x", recoveredAddr)
}

func (k *SignKeys) String() string { return k.EthAddrString() }

// Sign signs a message. Message is a normal string (no HexString nor a Hash)
func (k *SignKeys) Sign(message []byte) (string, error) {
	if k.Private.D == nil {
		return "", errors.New("no private key available")
	}
	signature, err := crypto.Sign(Hash(message), &k.Private)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", signature), nil
}

// SignJSON signs a JSON message. Message is a struct interface
func (k *SignKeys) SignJSON(message interface{}) (string, error) {
	rawMsg, err := json.Marshal(message)
	if err != nil {
		return "", errors.New("unable to marshal message to sign: %s")
	}
	sig, err := k.Sign(rawMsg)
	if err != nil {
		return "", errors.New("error signing response body: %s")
	}
	prefixedSig := "0x" + util.TrimHex(sig)
	return prefixedSig, nil
}

// Verify verifies a message. Signature is HexString
func (k *SignKeys) Verify(message []byte, signHex string) (bool, error) {
	pubHex, err := PubKeyFromSignature(message, util.TrimHex(signHex))
	if err != nil {
		return false, err
	}
	signature, err := hex.DecodeString(signHex)
	if err != nil {
		return false, err
	}
	pub, err := hex.DecodeString(pubHex)
	if err != nil {
		return false, err
	}
	hash := Hash(message)
	result := crypto.VerifySignature(pub, hash, signature[:64])
	return result, nil
}

// VerifySender verifies if a message is sent by some Authorized address key
func (k *SignKeys) VerifySender(msg []byte, sigHex string) (bool, string, error) {
	recoveredAddr, err := AddrFromSignature(msg, sigHex)
	if err != nil {
		return false, "", err
	}
	for _, addr := range k.Authorized {
		if fmt.Sprintf("%x", addr) == recoveredAddr {
			return true, recoveredAddr, nil
		}
	}
	return false, recoveredAddr, nil
}

// VerifyJSONsender verifies if a JSON message is sent by some Authorized address key
func (k *SignKeys) VerifyJSONsender(msg interface{}, sigHex string) (bool, string, error) {
	rawMsg, err := json.Marshal(msg)
	if err != nil {
		return false, "", errors.New("unable to marshal message to sign: %s")
	}
	return k.VerifySender(rawMsg, sigHex)
}

// Standalone function for verify a message
func Verify(message []byte, signHex, pubHex string) (bool, error) {
	sk := new(SignKeys)
	return sk.Verify(message, signHex)
}

// Standaolone function to obtain the Ethereum address from a ECDSA public key
func AddrFromPublicKey(pubHex string) (string, error) {
	var pubHexDesc string
	var err error
	if len(pubHex) <= PubKeyLength {
		pubHexDesc, err = decompressPubKey(util.TrimHex(pubHex))
		if err != nil {
			return "", err
		}
	} else {
		pubHexDesc = pubHex
	}
	pubBytes, err := hex.DecodeString(pubHexDesc)
	if err != nil {
		return "", err
	}
	pub, err := crypto.UnmarshalPubkey(pubBytes)
	if err != nil {
		return "", err
	}
	recoveredAddr := [20]byte(crypto.PubkeyToAddress(*pub))
	return fmt.Sprintf("%x", recoveredAddr), nil
}

func PubKeyFromPrivateKey(privHex string) (string, error) {
	var s SignKeys
	if err := s.AddHexKey(privHex); err != nil {
		return "", err
	}
	pub, _ := s.HexString()
	return pub, nil
}

// PubKeyFromSignature recovers the ECDSA public key that created the signature of a message
func PubKeyFromSignature(msg []byte, sigHex string) (string, error) {
	if len(util.TrimHex(sigHex)) < SignatureLength || len(util.TrimHex(sigHex)) > SignatureLength+12 {
		return "", errors.New("signature length not correct")
	}
	sig, err := hex.DecodeString(util.TrimHex(sigHex))
	if err != nil {
		return "", err
	}
	if sig[64] > 1 {
		sig[64] -= 27
	}
	if sig[64] > 1 {
		return "", errors.New("bad recover ID byte")
	}
	pubKey, err := crypto.SigToPub(Hash(msg), sig)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", crypto.FromECDSAPub(pubKey)), nil
}

// AddrFromSignature recovers the Ethereum address that created the signature of a message
func AddrFromSignature(msg []byte, sigHex string) (string, error) {
	pubHex, err := PubKeyFromSignature(msg, sigHex)
	if err != nil {
		return "", err
	}
	pub, err := hexToPubKey(pubHex)
	if err != nil {
		return "", err
	}
	addr := crypto.PubkeyToAddress(*pub)
	return fmt.Sprintf("%x", addr), nil
}

// AddrFromJSONsignature recovers the Ethereum address that created the signature of a JSON message
func AddrFromJSONsignature(msg interface{}, sigHex string) (string, error) {
	rawMsg, err := json.Marshal(msg)
	if err != nil {
		return "", errors.New("unable to marshal message to sign: %s")
	}
	return AddrFromSignature(rawMsg, sigHex)
}

func hexToPubKey(pubHex string) (*ecdsa.PublicKey, error) {
	pubBytes, err := hex.DecodeString(util.TrimHex(pubHex))
	if err != nil {
		return new(ecdsa.PublicKey), err
	}
	return crypto.UnmarshalPubkey(pubBytes)
}

// Hash string data adding Ethereum prefix
func Hash(data []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s%d%s", SigningPrefix, len(data), data)
	return HashRaw(buf.Bytes())
}

// HashRaw hashes a string with no prefix
func HashRaw(data []byte) []byte {
	return crypto.Keccak256(data)
}

// HashPoseidon computes the Poseidon hash of the given input.
// If an error happened, an empty byte slice is returned
func HashPoseidon(input []byte) []byte {
	hashNum, err := poseidon.HashBytes(input)
	if err != nil {
		return []byte{}
	}
	return i3utils.BigIntToHash(hashNum).Bytes()
}

// Encrypt uses secp256k1 standard from https://www.secg.org/sec2-v2.pdf to encrypt a message.
// The result is a Hexadecimal string
func (k *SignKeys) Encrypt(message string) (string, error) {
	pubKey := secp256k1.PublicKey(k.Public)
	ciphertext, err := secp256k1.Encrypt(&pubKey, []byte(message))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", ciphertext), nil
}

// Decrypt uses secp256k1 standard to decrypt a Hexadecimal string message
// The result is plain text (no hex encoded)
func (k *SignKeys) Decrypt(hexMessage string) (string, error) {
	cipertext, err := hex.DecodeString(util.TrimHex(hexMessage))
	if err != nil {
		return "", err
	}
	privKey := secp256k1.PrivateKey(k.Private)
	plaintext, err := secp256k1.Decrypt(&privKey, cipertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// CreateEthRandomKeysBatch creates a set of eth random signing keys
func CreateEthRandomKeysBatch(n int) ([]*SignKeys, error) {
	s := make([]*SignKeys, n)
	for i := 0; i < n; i++ {
		s[i] = new(SignKeys)
		if err := s[i].Generate(); err != nil {
			return nil, err
		}
	}
	return s, nil
}
