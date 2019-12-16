// Package census provides the census management operation
package census

import (
	"encoding/base64"
	"encoding/json"

	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"gitlab.com/vocdoni/go-dvote/types"

	"gitlab.com/vocdoni/go-dvote/crypto/signature"
	"gitlab.com/vocdoni/go-dvote/data"
	"gitlab.com/vocdoni/go-dvote/log"
	"gitlab.com/vocdoni/go-dvote/tree"
)

type CensusNamespaces struct {
	RootKey    string      `json:"rootKey"` // Public key allowed to created new census
	Namespaces []Namespace `json:"namespaces"`
}

type Namespace struct {
	Name string   `json:"name"`
	Keys []string `json:"keys"`
}

type CensusManager struct {
	Storage    string                // Root storage data dir
	AuthWindow int32                 // Time window (seconds) in which TimeStamp will be accepted if auth enabled
	Census     CensusNamespaces      // Available namespaces
	Trees      map[string]*tree.Tree // MkTrees map of merkle trees indexed by censusId
	Data       data.Storage
}

// Init creates a new census manager
func (cm *CensusManager) Init(storage, rootKey string) error {
	nsConfig := fmt.Sprintf("%s/namespaces.json", storage)
	cm.Storage = storage
	cm.Trees = make(map[string]*tree.Tree)
	cm.AuthWindow = 10

	log.Infof("loading namespaces and keys from %s", nsConfig)
	if _, err := os.Stat(nsConfig); os.IsNotExist(err) {
		log.Info("creating new config file")
		var cns CensusNamespaces
		if len(rootKey) < signature.PubKeyLength {
			// log.Warn("no root key provided or invalid, anyone will be able to create new census")
			cns.RootKey = ""
		} else {
			cns.RootKey = rootKey
		}
		cm.Census = cns
		ioutil.WriteFile(nsConfig, []byte(""), 0644)
		err = cm.save()
		return err
	}

	jsonFile, err := os.Open(nsConfig)
	if err != nil {
		return err
	}
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)
	err = json.Unmarshal(byteValue, &cm.Census)
	if err != nil {
		log.Warn("could not unmarshal json config file, probably empty. Skipping")
		return nil
	}
	if len(rootKey) >= signature.PubKeyLength {
		log.Infof("updating root key to %s", rootKey)
		cm.Census.RootKey = rootKey
	} else {
		log.Infof("current root key %s", rootKey)
	}
	// Initialize existing merkle trees
	for _, ns := range cm.Census.Namespaces {
		t := tree.Tree{}
		t.Storage = cm.Storage
		err := t.Init(ns.Name)
		if err != nil {
			log.Warn(err)
		} else {
			log.Infof("initialized merkle tree %s", ns.Name)
			cm.Trees[ns.Name] = &t
		}
	}
	return nil
}

// AddNamespace adds a new merkletree identified by a censusId (name)
func (cm *CensusManager) AddNamespace(name string, pubKeys []string) error {
	log.Infof("adding namespace %s", name)
	if _, e := cm.Trees[name]; e {
		return errors.New("namespace already exist")
	}
	mkTree := tree.Tree{}
	mkTree.Storage = cm.Storage
	err := mkTree.Init(name)
	if err != nil {
		return err
	}
	cm.Trees[name] = &mkTree
	var ns Namespace
	ns.Name = name
	ns.Keys = pubKeys
	cm.Census.Namespaces = append(cm.Census.Namespaces, ns)
	return cm.save()
}

func (cm *CensusManager) save() error {
	log.Info("saving namespaces")
	nsConfig := fmt.Sprintf("%s/namespaces.json", cm.Storage)
	data, err := json.Marshal(cm.Census)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(nsConfig, data, 0644)
}

func httpReply(resp *types.ResponseMessage, w http.ResponseWriter) {
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		http.Error(w, err.Error(), 500)
	} else {
		w.Header().Set("content-type", "application/json")
	}
}

func checkRequest(w http.ResponseWriter, req *http.Request) bool {
	if req.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return false
	}
	return true
}

// CheckAuth check if a census request message is authorized
func (cm *CensusManager) CheckAuth(rm *types.RequestMessage) error {
	if len(rm.Signature) < signature.SignatureLength || len(rm.CensusID) < 1 {
		return errors.New("signature or censusId not provided or invalid")
	}
	ns := new(Namespace)
	for _, n := range cm.Census.Namespaces {
		if n.Name == rm.CensusID {
			ns = &n
		}
	}

	// Add root key, if method is addCensus
	if rm.Method == "addCensus" {
		if len(cm.Census.RootKey) < signature.PubKeyLength {
			log.Warn("root key does not exist, considering addCensus valid for any request")
			return nil
		}
		ns.Keys = []string{cm.Census.RootKey}
	}

	if ns == nil {
		return errors.New("censusId not valid")
	}

	// Check timestamp
	currentTime := int32(time.Now().Unix())
	if rm.Timestamp > currentTime+cm.AuthWindow ||
		rm.Timestamp < currentTime-cm.AuthWindow {
		return errors.New("timestamp is not valid")
	}

	// Check signature with existing namespace keys
	log.Debugf("namespace keys %s", ns.Keys)
	if len(ns.Keys) > 0 {
		if len(ns.Keys) == 1 && len(ns.Keys[0]) < signature.PubKeyLength {
			log.Warnf("namespace %s does have management public key configured, allowing all", ns.Name)
			return nil
		}
		valid := false
		msg, err := json.Marshal(rm.MetaRequest)
		if err != nil {
			return errors.New("cannot unmarshal")
		}
		for _, n := range ns.Keys {
			valid, err = signature.Verify(string(msg), rm.Signature, n)
			if err != nil {
				log.Warnf("verification error (%s)", err)
				valid = false
			} else if valid {
				return nil
			}
		}
		if !valid {
			return errors.New("unauthorized")
		}
	} else {
		log.Warnf("namespace %s does have management public key configured, allowing all", ns.Name)
	}
	return nil
}

// HTTPhandler handles an API census manager request via HTTP
func (cm *CensusManager) HTTPhandler(w http.ResponseWriter, req *http.Request, signer *signature.SignKeys) {
	log.Debug("new request received")
	var rm types.RequestMessage
	if ok := checkRequest(w, req); !ok {
		return
	}
	// Decode JSON
	log.Debug("decoding JSON")
	err := json.NewDecoder(req.Body).Decode(&rm)
	if err != nil {
		log.Warnf("cannot decode JSON: %s", err)
		http.Error(w, err.Error(), 400)
		return
	}
	if len(rm.Method) < 1 {
		http.Error(w, "method must be specified", 400)
		return
	}
	log.Debugf("found method %s", rm.Method)
	auth := true
	err = cm.CheckAuth(&rm)
	if err != nil {
		log.Warnf("authorization error: %s", err)
		auth = false
	}
	resp := cm.Handler(&rm.MetaRequest, auth, "")
	respMsg := new(types.ResponseMessage)
	respMsg.MetaResponse = *resp
	respMsg.ID = rm.ID
	respMsg.Request = rm.ID
	respMsg.Signature, err = signer.SignJSON(respMsg.MetaResponse)
	if err != nil {
		log.Warn(err)
	}
	httpReply(respMsg, w)
}

// Handler handles an API census manager request.
// isAuth gives access to the private methods only if censusPrefix match or censusPrefix not defined
// censusPrefix should usually be the Ethereum Address or a Hash of the allowed PubKey
func (cm *CensusManager) Handler(r *types.MetaRequest, isAuth bool, censusPrefix string) *types.MetaResponse {
	resp := new(types.MetaResponse)
	op := r.Method
	var err error

	// Process data
	log.Infof("processing data %+v", *r)
	resp.Ok = true
	resp.Error = new(string)
	*resp.Error = ""
	resp.Timestamp = int32(time.Now().Unix())

	if op == "addCensus" {
		if isAuth {
			err = cm.AddNamespace(censusPrefix+r.CensusID, r.PubKeys)
			if err != nil {
				log.Warnf("error creating census: %s", err)
				resp.Ok = false
				*resp.Error = err.Error()
			} else {
				log.Infof("census %s%s created successfully managed by %s", censusPrefix, r.CensusID, r.PubKeys)
				resp.CensusID = censusPrefix + r.CensusID
			}
		} else {
			resp.Ok = false
			*resp.Error = "invalid authentication"
		}
		return resp
	}

	censusFound := false
	for k := range cm.Trees {
		if k == r.CensusID {
			censusFound = true
			break
		}
	}
	if !censusFound {
		resp.Ok = false
		*resp.Error = "censusId not valid or not found"
		return resp
	}

	// validAuthPrefix is true: either censusPrefix is not used or censusID contains the prefix
	validAuthPrefix := false
	if len(censusPrefix) == 0 {
		validAuthPrefix = true
		log.Debugf("prefix not specified, allowing access to all census IDs if pubkey validation correct")
	} else {
		validAuthPrefix = strings.HasPrefix(r.CensusID, censusPrefix)
		log.Debugf("prefix allowed for %s", r.CensusID)
	}

	// Methods without rootHash
	switch op {
	case "getRoot":
		resp.Root = cm.Trees[r.CensusID].Root()
		return resp

	case "addClaimBulk":
		if isAuth && validAuthPrefix {
			addedClaims := 0
			var invalidClaims []int
			for i, c := range r.ClaimsData {
				data, err := base64.StdEncoding.DecodeString(c)
				if err == nil {
					err = cm.Trees[r.CensusID].AddClaim(data)
				}
				if err != nil {
					log.Warnf("error adding claim: %s", err)
					invalidClaims = append(invalidClaims, i)
				} else {
					log.Debugf("claim added %x", data)
					addedClaims++
				}
			}
			if len(invalidClaims) > 0 {
				resp.InvalidClaims = invalidClaims
			}
			log.Infof("%d claims addedd successfully", addedClaims)
		} else {
			resp.Ok = false
			*resp.Error = "invalid authentication"
		}
		return resp

	case "addClaim":
		if isAuth && validAuthPrefix {
			data, err := base64.StdEncoding.DecodeString(r.ClaimData)
			if err != nil {
				log.Warnf("error decoding base64 string: %s", err)
				resp.Ok = false
				*resp.Error = err.Error()
			}
			err = cm.Trees[r.CensusID].AddClaim(data)
			if err != nil {
				log.Warnf("error adding claim: %s", err)
				resp.Ok = false
				*resp.Error = err.Error()
			} else {
				log.Debugf("claim added %x", data)
			}
		} else {
			resp.Ok = false
			*resp.Error = "invalid authentication"
		}
		return resp

	case "importDump":
		if isAuth && validAuthPrefix {
			if len(r.ClaimsData) > 0 {
				err = cm.Trees[r.CensusID].ImportDump(r.ClaimsData)
				if err != nil {
					log.Warnf("error importing dump: %s", err)
					resp.Ok = false
					*resp.Error = err.Error()
				} else {
					log.Infof("dump imported successfully, %d claims", len(r.ClaimsData))
				}
			}
		} else {
			resp.Ok = false
			*resp.Error = "invalid authentication"
		}
		return resp

	case "importRemote":
		// To-Do implement Gzip compression
		if !isAuth || !validAuthPrefix {
			resp.Ok = false
			*resp.Error = "invalid authentication"
			return resp
		}
		if cm.Data == nil {
			resp.Ok = false
			*resp.Error = "not supported"
			return resp
		}
		if !strings.HasPrefix(r.URI, cm.Data.URIprefix()) ||
			len(r.URI) <= len(cm.Data.URIprefix()) {
			log.Warnf("uri not supported %s (supported prefix %s)", r.URI, cm.Data.URIprefix())
			resp.Ok = false
			*resp.Error = "URI not supported"
			return resp
		}
		log.Infof("retrieving remote census %s", r.CensusURI)
		censusRaw, err := cm.Data.Retrieve(r.URI[len(cm.Data.URIprefix()):])
		if err != nil {
			log.Warnf("cannot retrieve census: %s", err)
			resp.Ok = false
			*resp.Error = "cannot retrieve census"
			return resp
		}
		var dump types.CensusDump
		err = json.Unmarshal(censusRaw, &dump)
		if err != nil {
			log.Warnf("retrieved census do not have a correct format: %s", err)
			resp.Ok = false
			*resp.Error = "retrieved census do not have a correct format"
			return resp
		}
		log.Infof("retrieved census with rootHash %s and size %d bytes", dump.RootHash, len(censusRaw))
		if len(dump.ClaimsData) > 0 {
			err = cm.Trees[r.CensusID].ImportDump(dump.ClaimsData)
			if err != nil {
				log.Warnf("error importing dump: %s", err)
				resp.Ok = false
				*resp.Error = "error importing census"
			} else {
				log.Infof("dump imported successfully, %d claims", len(dump.ClaimsData))
			}
		} else {
			log.Warnf("no claims found on the retreived census")
			resp.Ok = false
			*resp.Error = "no claims found"
		}
		return resp

	case "checkProof":
		if len(r.Payload.Proof) < 1 {
			resp.Ok = false
			*resp.Error = "proofData not provided"
			return resp
		}
		root := r.RootHash
		if len(root) < 1 {
			root = cm.Trees[r.CensusID].Root()
		}
		// Generate proof and return it
		data, err := base64.StdEncoding.DecodeString(r.ClaimData)
		if err != nil {
			log.Warnf("error decoding base64 string: %s", err)
			resp.Ok = false
			*resp.Error = err.Error()
			return resp
		}
		validProof, err := tree.CheckProof(root, r.Payload.Proof, data)
		if err != nil {
			resp.Ok = false
			*resp.Error = err.Error()
			return resp
		}
		resp.ValidProof = validProof
		return resp
	}

	// Methods with rootHash, if rootHash specified snapshot the tree
	var t *tree.Tree
	if len(r.RootHash) > 1 { // if rootHash specified
		t, err = cm.Trees[r.CensusID].Snapshot(r.RootHash)
		if err != nil {
			log.Warnf("snapshot error: %s", err)
			resp.Ok = false
			*resp.Error = "invalid root hash"
			return resp
		}
	} else { // if rootHash not specified use current tree
		t = cm.Trees[r.CensusID]
	}

	switch op {
	case "genProof":
		data, err := base64.StdEncoding.DecodeString(r.ClaimData)
		if err != nil {
			log.Warnf("error decoding base64 string: %s", err)
			resp.Ok = false
			*resp.Error = err.Error()
			return resp
		}
		resp.Siblings, err = t.GenProof(data)
		if err != nil {
			resp.Ok = false
			*resp.Error = err.Error()
		}
		return resp

	case "getSize":
		resp.Size, err = t.Size(t.Root())
		if err != nil {
			resp.Ok = false
			*resp.Error = err.Error()
		}
		return resp

	case "dump", "dumpPlain":
		if !isAuth || !validAuthPrefix {
			resp.Ok = false
			*resp.Error = "invalid authentication"
			return resp
		}
		// dump the claim data and return it
		var dumpValues []string
		root := r.RootHash
		if len(root) < 1 {
			root = t.Root()
		}
		if op == "dump" {
			dumpValues, err = t.Dump(root)
		} else {
			dumpValues, err = t.DumpPlain(root, true)
		}
		if err != nil {
			*resp.Error = err.Error()
			resp.Ok = false
		} else {
			resp.ClaimsData = dumpValues
		}
		return resp

	case "publish":
		// To-Do implement Gzip compression
		if !isAuth || !validAuthPrefix {
			resp.Ok = false
			*resp.Error = "invalid authentication"
			return resp
		}
		if cm.Data == nil {
			resp.Ok = false
			*resp.Error = "not supported"
			return resp
		}
		var dump types.CensusDump
		dump.RootHash = t.Root()
		dump.ClaimsData, err = t.Dump(t.Root())
		if err != nil {
			*resp.Error = err.Error()
			resp.Ok = false
			log.Warnf("cannot dump census with root %s: %s", t.Root(), err)
			return resp
		}
		dumpBytes, err := json.Marshal(dump)
		if err != nil {
			*resp.Error = err.Error()
			resp.Ok = false
			log.Warnf("cannot marshal census dump: %s", err)
			return resp
		}
		cid, err := cm.Data.Publish(dumpBytes)
		if err != nil {
			*resp.Error = err.Error()
			resp.Ok = false
			log.Warnf("cannot publish census dump: %s", err)
			return resp
		}
		resp.URI = cm.Data.URIprefix() + cid
		log.Infof("published census at %s", resp.URI)
		resp.Root = t.Root()

		// adding published census with censusID = rootHash
		log.Infof("adding new namespace for published census %s", resp.Root)
		err = cm.AddNamespace(resp.Root, r.PubKeys)
		if err != nil {
			log.Warnf("error creating local published census: %s", err)
		} else {
			log.Infof("import claims to new census")
			err = cm.Trees[resp.Root].ImportDump(dump.ClaimsData)
			if err != nil {
				log.Warn(err)
			}
		}
	}

	return resp
}
