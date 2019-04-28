package search

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"

	lru "github.com/hashicorp/golang-lru"
	"github.com/keybase/client/go/chat/globals"
	"github.com/keybase/client/go/chat/storage"
	"github.com/keybase/client/go/chat/utils"
	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/chat1"
	"github.com/keybase/client/go/protocol/gregor1"
)

const indexVersion = 4
const tokenEntryVersion = 2
const aliasEntryVersion = 1

type tokenEntry struct {
	Version string                                `codec:"v"`
	MsgIDs  map[chat1.MessageID]chat1.EmptyStruct `codec:"m"`
}

func newTokenEntry() *tokenEntry {
	return &tokenEntry{
		Version: fmt.Sprintf("%s:%s", indexVersion, tokenEntryVersion),
		MsgIDs:  make(map[chat1.MessageID]chat1.EmptyStruct),
	}
}

var refTokenEntry = newTokenEntry()

type aliasEntry struct {
	Version string                       `codec:"v"`
	Aliases map[string]chat1.EmptyStruct `codec:"a"`
}

func newAliasEntry() *aliasEntry {
	return &aliasEntry{
		Version: fmt.Sprintf("%s:%s", indexVersion, aliasEntryVersion),
		Aliases: make(map[string]chat1.EmptyStruct),
	}
}

var refAliasEntry = newAliasEntry()

type store struct {
	utils.DebugLabeler
	lockTab *libkb.LockTable
	globals.Contextified
	keyFn      func(ctx context.Context) ([32]byte, error)
	aliasCache *lru.Cache
}

func newStore(g *globals.Context) *store {
	ac, _ := lru.New(100000)
	return &store{
		Contextified: globals.NewContextified(g),
		DebugLabeler: utils.NewDebugLabeler(g.GetLog(), "Search.store", false),
		lockTab:      &libkb.LockTable{},
		keyFn: func(ctx context.Context) ([32]byte, error) {
			return storage.GetSecretBoxKey(ctx, g.ExternalG(), storage.DefaultSecretUI)
		},
		aliasCache: ac,
	}
}

func (s *store) metadataKey(uid gregor1.UID, convID chat1.ConversationID) libkb.DbKey {
	return libkb.DbKey{
		Typ: libkb.DBChatIndex,
		Key: fmt.Sprintf("md:%s:%s", uid, convID),
	}
}

func (s *store) entryKey(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID, dat, name string) (res libkb.DbKey, err error) {
	material, err := s.keyFn(ctx)
	if err != nil {
		return res, err
	}
	termPart := append([]byte(dat), material[:]...)
	termPartBytes := hmac.New(sha256.New, termPart).Sum(nil)
	return libkb.DbKey{
		Typ: libkb.DBChatIndex,
		Key: fmt.Sprintf("%s:%s:%s:%s", name, uid, convID, termPartBytes),
	}, nil
}

func (s *store) termKey(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID, dat string) (res libkb.DbKey, err error) {
	return s.entryKey(ctx, uid, convID, dat, "term")
}

func (s *store) aliasKey(ctx context.Context, dat string) (res libkb.DbKey, err error) {
	material, err := s.keyFn(ctx)
	if err != nil {
		return res, err
	}
	termPart := append([]byte(dat), material[:]...)
	termPartBytes := hmac.New(sha256.New, termPart).Sum(nil)
	return libkb.DbKey{
		Typ: libkb.DBChatIndex,
		Key: fmt.Sprintf("alias:%s", termPartBytes),
	}, nil
}

func (s *store) getTermAliases(ctx context.Context, term string) (aliases map[string]chat1.EmptyStruct, err error) {
	aliasKey, err := s.aliasKey(ctx, term)
	if err != nil {
		return aliases, err
	}
	ae, err := s.getAliasEntry(ctx, aliasKey)
	if err != nil {
		return aliases, err
	}
	return ae.Aliases, nil
}

func (s *store) getHits(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID, term string) (res map[chat1.MessageID]chat1.EmptyStruct, err error) {
	res = make(map[chat1.MessageID]chat1.EmptyStruct)
	// Get all terms and aliases
	terms := make(map[string]chat1.EmptyStruct)
	aliases, err := s.getTermAliases(ctx, term)
	if err != nil {
		return res, err
	}
	terms[term] = chat1.EmptyStruct{}
	for alias := range aliases {
		terms[alias] = chat1.EmptyStruct{}
	}
	// Get all the keys
	var keys []libkb.DbKey
	for term := range terms {
		key, err := s.termKey(ctx, uid, convID, term)
		if err != nil {
			return res, err
		}
		keys = append(keys, key)
	}
	// Find all the msg IDs
	for _, key := range keys {
		te, err := s.getTokenEntry(ctx, key)
		if err != nil {
			return nil, err
		}
		for msgID := range te.MsgIDs {
			res[msgID] = chat1.EmptyStruct{}
		}
	}
	return res, nil
}

func (s *store) getTokenEntry(ctx context.Context, key libkb.DbKey) (res *tokenEntry, err error) {
	var te tokenEntry
	found, err := s.G().LocalChatDb.GetIntoMsgpack(&te, key)
	if err != nil {
		return nil, err
	}
	if !found {
		return newTokenEntry(), nil
	}
	res = &te
	if res.Version != refTokenEntry.Version {
		return newTokenEntry(), nil
	}
	return res, nil
}

func (s *store) getAliasEntry(ctx context.Context, key libkb.DbKey) (res *aliasEntry, err error) {
	var ae aliasEntry
	if dat, ok := s.aliasCache.Get(key); ok {
		return dat.(*aliasEntry), nil
	}
	found, err := s.G().LocalChatDb.GetIntoMsgpack(&ae, key)
	if err != nil {
		return nil, err
	}
	if !found {
		return newAliasEntry(), nil
	}
	res = &ae
	if res.Version != refAliasEntry.Version {
		return newAliasEntry(), nil
	}
	s.aliasCache.Add(key, res)
	return res, nil
}

func (s *store) putTokenEntry(ctx context.Context, key libkb.DbKey, te *tokenEntry) error {
	return s.G().LocalChatDb.PutObjMsgpack(key, nil, te)
}

func (s *store) putAliasEntry(ctx context.Context, key libkb.DbKey, ae *aliasEntry) error {
	s.aliasCache.Remove(key)
	return s.G().LocalChatDb.PutObjMsgpack(key, nil, ae)
}

func (s *store) deleteEntry(ctx context.Context, key libkb.DbKey) {
	if err := s.G().LocalChatDb.Delete(key); err != nil {
		s.Debug(ctx, "deleteTokenEntry: failed to delete key: %s", err)
	}
}

func (s *store) getAliasKeys(ctx context.Context, aliases map[string]chat1.EmptyStruct) (res []libkb.DbKey, err error) {
	for alias := range aliases {
		aliasKey, err := s.aliasKey(ctx, alias)
		if err != nil {
			return res, err
		}
		res = append(res, aliasKey)
	}
	return res, nil
}

// addTokens add the given tokens to the index under the given message
// id, when ingesting EDIT messages the msgID is of the superseded msg but the
// tokens are from the EDIT itself.
func (s *store) addTokensLocked(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	tokens tokenMap, msgID chat1.MessageID) error {
	for token, aliases := range tokens {
		tokenKey, err := s.termKey(ctx, uid, convID, token)
		if err != nil {
			return err
		}
		// Update the token entry with the msg ID hit
		te, err := s.getTokenEntry(ctx, tokenKey)
		if err != nil {
			return err
		}
		if _, ok := te.MsgIDs[msgID]; !ok {
			te.MsgIDs[msgID] = chat1.EmptyStruct{}
			s.Debug(ctx, "addTokensLocked: token write: %s id: %d", token, msgID)
			if err := s.putTokenEntry(ctx, tokenKey, te); err != nil {
				return err
			}
		}

		// Update all the aliases to point at the token
		aliasKeys, err := s.getAliasKeys(ctx, aliases)
		if err != nil {
			return err
		}
		for _, aliasKey := range aliasKeys {
			aliasEntry, err := s.getAliasEntry(ctx, aliasKey)
			if err != nil {
				return err
			}
			if _, ok := aliasEntry.Aliases[token]; ok {
				continue
			}
			aliasEntry.Aliases[token] = chat1.EmptyStruct{}
			s.Debug(ctx, "addTokensLocked: alias write: %s", token)
			if err := s.putAliasEntry(ctx, aliasKey, aliasEntry); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *store) addMsgLocked(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	msg chat1.MessageUnboxed) error {
	tokens := tokensFromMsg(msg)
	return s.addTokensLocked(ctx, uid, convID, tokens, msg.GetMessageID())
}

func (s *store) removeMsgLocked(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	msg chat1.MessageUnboxed) error {
	// find the msgID that the index stores
	var msgID chat1.MessageID
	switch msg.GetMessageType() {
	case chat1.MessageType_EDIT, chat1.MessageType_ATTACHMENTUPLOADED:
		superIDs, err := utils.GetSupersedes(msg)
		if err != nil || len(superIDs) != 1 {
			return err
		}
		msgID = superIDs[0]
	default:
		msgID = msg.GetMessageID()
	}

	for token, aliases := range tokensFromMsg(msg) {
		// handle token
		tokenKey, err := s.termKey(ctx, uid, convID, token)
		if err != nil {
			return err
		}
		te, err := s.getTokenEntry(ctx, tokenKey)
		if err != nil {
			return err
		}
		delete(te.MsgIDs, msgID)
		if len(te.MsgIDs) == 0 {
			s.deleteEntry(ctx, tokenKey)
		} else {
			// If there are still IDs, just write out the updated version
			if err := s.putTokenEntry(ctx, tokenKey, te); err != nil {
				return err
			}
		}
		// take out aliases
		aliasKeys, err := s.getAliasKeys(ctx, aliases)
		if err != nil {
			return err
		}
		for _, aliasKey := range aliasKeys {
			aliasEntry, err := s.getAliasEntry(ctx, aliasKey)
			if err != nil {
				return err
			}
			delete(aliasEntry.Aliases, token)
			if len(aliasEntry.Aliases) == 0 {
				s.deleteEntry(ctx, aliasKey)
			} else {
				if err := s.putAliasEntry(ctx, aliasKey, aliasEntry); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *store) getMetadata(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID) (res *indexMetadata, err error) {
	var md indexMetadata
	found, err := s.G().LocalChatDb.GetIntoMsgpack(&md, s.metadataKey(uid, convID))
	if err != nil {
		return res, err
	}
	if !found {
		return newIndexMetadata(), nil
	}
	if md.Version != refIndexMetadata.Version {
		return newIndexMetadata(), nil
	}
	return &md, nil
}

func (s *store) putMetadata(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	md *indexMetadata) error {
	return s.G().LocalChatDb.PutObjMsgpack(s.metadataKey(uid, convID), nil, md)
}

func (s *store) add(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	msgs []chat1.MessageUnboxed) (err error) {
	lock := s.lockTab.AcquireOnName(ctx, s.G(), convID.String())
	defer lock.Release(ctx)

	fetchSupersededMsgs := func(msg chat1.MessageUnboxed) []chat1.MessageUnboxed {
		superIDs, err := utils.GetSupersedes(msg)
		if err != nil {
			s.Debug(ctx, "unable to get supersedes: %v", err)
			return nil
		}
		reason := chat1.GetThreadReason_INDEXED_SEARCH
		supersededMsgs, err := s.G().ChatHelper.GetMessages(ctx, uid, convID, superIDs,
			false /* resolveSupersedes*/, &reason)
		if err != nil {
			// Log but ignore error
			s.Debug(ctx, "unable to get fetch messages: %v", err)
			return nil
		}
		return supersededMsgs
	}

	md, err := s.getMetadata(ctx, uid, convID)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		seenIDs := md.SeenIDs
		// Don't add if we've seen
		if _, ok := seenIDs[msg.GetMessageID()]; ok {
			continue
		}
		seenIDs[msg.GetMessageID()] = chat1.EmptyStruct{}
		// NOTE DELETE and DELETEHISTORY are handled through calls to `remove`,
		// other messages will be added if there is any content that can be
		// indexed.
		switch msg.GetMessageType() {
		case chat1.MessageType_ATTACHMENTUPLOADED:
			supersededMsgs := fetchSupersededMsgs(msg)
			for _, sm := range supersededMsgs {
				seenIDs[sm.GetMessageID()] = chat1.EmptyStruct{}
				s.addMsgLocked(ctx, uid, convID, sm)
			}
		case chat1.MessageType_EDIT:
			tokens := tokensFromMsg(msg)
			supersededMsgs := fetchSupersededMsgs(msg)
			// remove the original message text and replace it with the edited
			// contents (using the original id in the index)
			for _, sm := range supersededMsgs {
				seenIDs[sm.GetMessageID()] = chat1.EmptyStruct{}
				s.removeMsgLocked(ctx, uid, convID, sm)
				s.addTokensLocked(ctx, uid, convID, tokens, sm.GetMessageID())
			}
		default:
			s.addMsgLocked(ctx, uid, convID, msg)
		}
	}
	return s.putMetadata(ctx, uid, convID, md)
}

// Remove tokenizes the message content and updates/removes index keys for each token.
func (s *store) remove(ctx context.Context, uid gregor1.UID, convID chat1.ConversationID,
	msgs []chat1.MessageUnboxed) (err error) {
	lock := s.lockTab.AcquireOnName(ctx, s.G(), convID.String())
	defer lock.Release(ctx)

	md, err := s.getMetadata(ctx, uid, convID)
	if err != nil {
		return err
	}

	seenIDs := md.SeenIDs
	for _, msg := range msgs {
		// Don't remove if we haven't seen
		if _, ok := seenIDs[msg.GetMessageID()]; !ok {
			continue
		}
		seenIDs[msg.GetMessageID()] = chat1.EmptyStruct{}
		s.removeMsgLocked(ctx, uid, convID, msg)
	}
	err = s.putMetadata(ctx, uid, convID, md)
	return err
}
