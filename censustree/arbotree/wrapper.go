// Package arbotree provides the functions for creating and managing an arbo
// merkletree adapted to the CensusTree interface
package arbotree

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/vocdoni/arbo"
	"go.vocdoni.io/dvote/censustree"
	"go.vocdoni.io/dvote/db"
)

// Tree implements the censustree.Tree interface using arbo.Tree
// functionallities
type Tree struct {
	Tree           *arbo.Tree
	public         uint32
	lastAccessUnix int64 // a unix timestamp, used via sync/atomic
}

// check that censustree.Tree interface is matched by Tree
var _ censustree.Tree = (*Tree)(nil)

// NewTree opens or creates a merkle tree using the given storage, and the given
// hash function.
// Note that each tree should use an entirely separate namespace for its database keys.
func NewTree(name, storageDir string, nLevels int, hashFunc arbo.HashFunction) (
	censustree.Tree, error) {
	dbDir := filepath.Join(storageDir, "arbotree.db."+strings.TrimSpace(name))
	database, err := db.NewBadgerDB(dbDir)
	if err != nil {
		return nil, err
	}

	mt, err := arbo.NewTree(database, nLevels, hashFunc)
	if err != nil {
		return nil, err
	}
	tree := &Tree{
		Tree: mt,
	}
	tree.updateAccessTime()
	return tree, nil
}

// Type returns the name identifying the censustree implementation
func (t *Tree) Type() string {
	return fmt.Sprintf("arbo-%s", t.Tree.HashFunction().Type())
}

// FactoryID returns the numeric identifier of the censustree implementation
func (t *Tree) FactoryID() int {
	switch string(t.Tree.HashFunction().Type()) {
	case "blake2b":
		return 1
	case "poseidon":
		return 2
	}
	return 0
}

// Init initializes the Tree using the given storage directory, by default is
// used the Blake2b hash function. If another hash function is desired, should
// use NewTree method or even censustree/util.NewCensusTree method, which allows
// to choose the hash function to be used in the Tree.
func (t *Tree) Init(name, storageDir string) error {
	dbDir := filepath.Join(storageDir, "arbotree.db."+strings.TrimSpace(name))
	database, err := db.NewBadgerDB(dbDir)
	if err != nil {
		return err
	}

	mt, err := arbo.NewTree(database, 140, arbo.HashFunctionBlake2b)
	if err != nil {
		return err
	}
	t.Tree = mt
	t.updateAccessTime()
	return nil
}

// MaxKeySize returns the maximum key size supported by the Tree, which depends
// on the has function used
func (t *Tree) MaxKeySize() int {
	return t.Tree.HashFunction().Len()
}

// LastAccess returns the last time the Tree was accessed, in the form of a unix
// timestamp.
func (t *Tree) LastAccess() int64 {
	return atomic.LoadInt64(&t.lastAccessUnix)
}

func (t *Tree) updateAccessTime() {
	atomic.StoreInt64(&t.lastAccessUnix, time.Now().Unix())
}

// Publish makes a merkle tree available for queries.
// Application layer should check IsPublish() before considering the Tree available.
func (t *Tree) Publish() {
	atomic.StoreUint32(&t.public, 1)
}

// UnPublish makes a merkle tree not available for queries
func (t *Tree) UnPublish() {
	atomic.StoreUint32(&t.public, 0)
}

// IsPublic returns true if the tree is available
func (t *Tree) IsPublic() bool {
	return atomic.LoadUint32(&t.public) == 1
}

// Add adds a new leaf into the merkle tree.  A leaf is composed by the index
// and value, where index determines the position of the leaf in the tree.
func (t *Tree) Add(index, value []byte) error {
	t.updateAccessTime()
	return t.Tree.Add(index, value)
}

// AddBatch adds a batch of indexes and values to the tree using the AddBatch
// optimized method from arbo
func (t *Tree) AddBatch(indexes, values [][]byte) ([]int, error) {
	t.updateAccessTime()
	return t.Tree.AddBatch(indexes, values)
}

// GenProof generates a merkle tree proof that can be later used on CheckProof()
// to validate it
func (t *Tree) GenProof(index, value []byte) ([]byte, error) {
	t.updateAccessTime()
	_, v, siblings, existence, err := t.Tree.GenProof(index)
	if err != nil {
		return nil, err
	}
	if !existence {
		return nil, fmt.Errorf("index does not exist in the tree")
	}
	if !bytes.Equal(v, value) {
		return nil, fmt.Errorf("value does not match %s!=%s",
			hex.EncodeToString(v), hex.EncodeToString(value))
	}
	return siblings, nil
}

// CheckProof validates a merkle proof and its data for the Tree hash function
func (t *Tree) CheckProof(index, value, root, mproof []byte) (bool, error) {
	t.updateAccessTime()
	if root == nil {
		root = t.Root()
	}
	return arbo.CheckProof(t.Tree.HashFunction(), index, value, root, mproof)
}

// Root returns the current root hash of the merkle tree
func (t *Tree) Root() []byte {
	t.updateAccessTime()
	return t.Tree.Root()
}

// Dump exports all the Tree leafs in a byte array, which can later be imported
// using the ImportDump method
func (t *Tree) Dump(root []byte) ([]byte, error) {
	t.updateAccessTime()
	return t.Tree.Dump(root)
}

// DumpPlain returns all the Tree leafs in two arrays, one for the keys, and
// another for the values.
func (t *Tree) DumpPlain(root []byte) ([][]byte, [][]byte, error) {
	t.updateAccessTime()
	var indexes, values [][]byte
	err := t.Tree.Iterate(root, func(k, v []byte) {
		if v[0] != arbo.PrefixValueLeaf {
			return
		}
		leafK, leafV := arbo.ReadLeafValue(v)
		indexes = append(indexes, leafK)
		values = append(values, leafV)
	})
	return indexes, values, err
}

// ImportDump imports the leafs (that have been exported with the Dump method)
// in the Tree.
func (t *Tree) ImportDump(data []byte) error {
	t.updateAccessTime()
	return t.Tree.ImportDump(data)
}

// Size returns the number of leafs of the Tree. Be aware that with the current
// implementation it iterates over the full tree to count the leafs each time
// that the method is called.
func (t *Tree) Size(root []byte) (int64, error) {
	t.updateAccessTime()
	count := 0
	err := t.Tree.Iterate(root, func(k, v []byte) {
		if v[0] != arbo.PrefixValueLeaf {
			return
		}
		count++
	})
	return int64(count), err
}

// Snapshot returns a censustree.Tree instance of the Tree, for a given merkle
// root. if the root parameter is nil, it will use the current root of the tree.
func (t *Tree) Snapshot(root []byte) (censustree.Tree, error) {
	t.updateAccessTime()
	snapshot, err := t.Tree.Snapshot(root)
	if err != nil {
		return t, err
	}
	return &Tree{
		Tree:           snapshot,
		public:         t.public,
		lastAccessUnix: time.Now().Unix(),
	}, nil
}

// HashExists checks if a hash exists as a key of a node in the merkle tree
func (t *Tree) HashExists(hash []byte) (bool, error) {
	t.updateAccessTime()
	if _, _, err := t.Tree.Get(hash); err != nil {
		return false, err
	}
	return true, nil
}
