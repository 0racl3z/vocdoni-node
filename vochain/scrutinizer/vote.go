package scrutinizer

import (
	"encoding/json"
	"fmt"

	"github.com/dgraph-io/badger/v2"

	"gitlab.com/vocdoni/go-dvote/crypto/nacl"
	"gitlab.com/vocdoni/go-dvote/log"
	"gitlab.com/vocdoni/go-dvote/types"
)

// ErrNoResultsYet is an error returned to indicate the process exist but it does not have yet reuslts
var ErrNoResultsYet = fmt.Errorf("no results yet")

// unmarshalVote decodes the base64 payload to a VotePackage struct type.
// If the votePackage is encrypted the list of keys to decrypt it should be provided.
// The order of the Keys must be as it was encrypted.
// The function will reverse the order and use the decryption keys starting from the last one provided.
func unmarshalVote(votePackage []byte, keys []string) (*types.VotePackage, error) {
	var vote types.VotePackage
	rawVote := make([]byte, len(votePackage))
	copy(rawVote, votePackage)
	// if encryption keys, decrypt the vote
	if len(keys) > 0 {
		for i := len(keys) - 1; i >= 0; i-- {
			priv, err := nacl.DecodePrivate(keys[i])
			if err != nil {
				return nil, fmt.Errorf("cannot create private key cipher: (%s)", err)
			}
			if rawVote, err = priv.Decrypt(rawVote); err != nil {
				return nil, fmt.Errorf("cannot decrypt vote with index key %d: %w", i, err)
			}
		}
	}
	if err := json.Unmarshal(rawVote, &vote); err != nil {
		return nil, fmt.Errorf("cannot unmarshal vote: %w", err)
	}
	return &vote, nil
}

func (s *Scrutinizer) addLiveResultsVote(envelope *types.Vote) error {
	pid := envelope.ProcessID
	if pid == nil {
		return fmt.Errorf("cannot find process for envelope")
	}
	vote, err := unmarshalVote(envelope.VotePackage, []string{})
	if err != nil {
		return err
	}
	if len(vote.Votes) > MaxQuestions {
		return fmt.Errorf("too many questions on addVote")
	}

	process, err := s.Storage.Get(s.encode("liveProcess", pid))
	if err != nil {
		return fmt.Errorf("error adding vote to process %x, skipping addVote: (%s)", pid, err)
	}

	var pv ProcessVotes
	if err := s.VochainState.Codec.UnmarshalBinaryBare(process, &pv); err != nil {
		return fmt.Errorf("cannot unmarshal vote (%s)", err)
	}

	for question, opt := range vote.Votes {
		if opt > MaxOptions {
			log.Warn("option overflow on addVote")
			continue
		}
		pv[question][opt]++
	}

	process, err = s.VochainState.Codec.MarshalBinaryBare(pv)
	if err != nil {
		return err
	}

	if err := s.Storage.Put(s.encode("process", pid), process); err != nil {
		return err
	}

	log.Debugf("addVote on process %x", pid)
	return nil
}

// ComputeResult process a finished voting, compute the results and saves it in the Storage
func (s *Scrutinizer) ComputeResult(processID []byte) error {
	log.Debugf("computing results for %x", processID)
	// Check if process exist
	p, err := s.VochainState.Process(processID, false)
	if err != nil {
		return err
	}

	// If result already exist, skipping
	_, err = s.Storage.Get(s.encode("results", processID))
	if err == nil {
		return fmt.Errorf("process %x already computed", processID)
	}
	if err != nil && err != badger.ErrKeyNotFound {
		return err
	}

	// Compute the results
	// If poll-vote, results have been computed during their arrival
	isLive, err := s.isLiveResultsProcess(processID)
	if err != nil {
		return err
	}
	var pv ProcessVotes
	if isLive {
		if pv, err = s.computeLiveResults(processID); err != nil {
			return err
		}
		// Delete liveResults temporary storage
		if err = s.Storage.Del(s.encode("liveProcess", processID)); err != nil {
			return err
		}
	} else {
		if pv, err = s.computeNonLiveResults(processID, p); err != nil {
			return err
		}
	}

	result, err := s.VochainState.Codec.MarshalBinaryBare(pv)
	if err != nil {
		return err
	}

	return s.Storage.Put(s.encode("results", processID), result)
}

// VoteResult returns the current result for a processId summarized in a two dimension int slice
func (s *Scrutinizer) VoteResult(processID []byte) (ProcessVotes, error) {
	// Check if process exist
	_, err := s.VochainState.Process(processID, false)
	if err != nil {
		return nil, err
	}

	log.Debugf("finding results for %x", processID)
	// If exist a summary of the voting process, just return it
	var pv ProcessVotes
	processBytes, err := s.Storage.Get(s.encode("results", processID))
	if err != nil && err != badger.ErrKeyNotFound {
		return nil, err
	}
	if err == nil {
		if err := s.VochainState.Codec.UnmarshalBinaryBare(processBytes, &pv); err != nil {
			return nil, err
		}
		return pv, nil
	}

	// If results are not available, check if the process is PollVote (live)
	isLive, err := s.isLiveResultsProcess(processID)
	if err != nil {
		return nil, err
	}
	if !isLive {
		return nil, ErrNoResultsYet
	}

	// Return live results
	return s.computeLiveResults(processID)
}

func (s *Scrutinizer) computeLiveResults(processID []byte) (pv ProcessVotes, err error) {
	var pb []byte
	pb, err = s.Storage.Get(s.encode("liveProcess", processID))
	if err != nil {
		return
	}
	if err = s.VochainState.Codec.UnmarshalBinaryBare(pb, &pv); err != nil {
		return
	}
	pruneVoteResult(&pv)
	log.Debugf("computed live results for %x", processID)
	return
}

func (s *Scrutinizer) computeNonLiveResults(processID []byte, p *types.Process) (pv ProcessVotes, err error) {
	pv = emptyProcess()
	var nvotes int
	for _, e := range s.VochainState.EnvelopeList(processID, 0, 32<<18, false) { // 8.3M seems enough for now
		v, err := s.VochainState.Envelope(processID, e, false)
		if err != nil {
			log.Warn(err)
			continue
		}
		var vp *types.VotePackage
		err = nil
		if p.IsEncrypted() {
			if len(p.EncryptionPrivateKeys) < len(v.EncryptionKeyIndexes) {
				err = fmt.Errorf("encryptionKeyIndexes has too many fields")
			} else {
				keys := []string{}
				for _, k := range v.EncryptionKeyIndexes {
					if k >= types.MaxKeyIndex {
						err = fmt.Errorf("key index overflow")
						break
					}
					keys = append(keys, p.EncryptionPrivateKeys[k])
				}
				if len(keys) == 0 || err != nil {
					err = fmt.Errorf("no keys provided or wrong index")
				} else {
					vp, err = unmarshalVote(v.VotePackage, keys)
				}
			}
		} else {
			vp, err = unmarshalVote(v.VotePackage, []string{})
		}
		if err != nil {
			log.Warn(err)
			continue
		}
		for question, opt := range vp.Votes {
			if opt > MaxOptions {
				log.Warn("option overflow on computeResult, skipping vote...")
				continue
			}
			pv[question][opt]++
		}
		nvotes++
	}
	pruneVoteResult(&pv)
	log.Infof("computed results for process %x with %d votes", processID, nvotes)
	return
}

// To-be-improved
func pruneVoteResult(pv *ProcessVotes) {
	pvv := *pv
	var pvc ProcessVotes
	min := MaxQuestions - 1
	for ; min >= 0; min-- { // find the real size of first dimension (questions with some answer)
		j := 0
		for ; j < MaxOptions; j++ {
			if pvv[min][j] != 0 {
				break
			}
		}
		if j < MaxOptions {
			break
		} // we found a non-empty question, this is the min. Stop iteration.
	}

	for i := 0; i <= min; i++ { // copy the options for each question but pruning options too
		pvc = make([][]uint32, i+1)
		for i2 := 0; i2 <= i; i2++ { // copy only the first non-zero values
			j2 := MaxOptions - 1
			for ; j2 >= 0; j2-- {
				if pvv[i2][j2] != 0 {
					break
				}
			}
			pvc[i2] = make([]uint32, j2+1)
			copy(pvc[i2], pvv[i2])
		}
	}
	*pv = pvc
}
