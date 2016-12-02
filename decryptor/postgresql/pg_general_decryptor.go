// Copyright 2016, Cossack Labs Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package postgresql

import (
	"github.com/cossacklabs/acra/decryptor/base"
	"github.com/cossacklabs/acra/decryptor/binary"
	"github.com/cossacklabs/acra/keystore"
	"github.com/cossacklabs/acra/zone"
	"github.com/cossacklabs/themis/gothemis/keys"
	"io"
	"log"
)

type PgDecryptor struct {
	is_with_zone      bool
	key_store         keystore.KeyStore
	zone_matcher      *zone.ZoneIdMatcher
	pg_decryptor      base.DataDecryptor
	binary_decryptor  base.DataDecryptor
	matched_decryptor base.DataDecryptor

	poison_key       []byte
	client_id        []byte
	match_buffer     []byte
	match_index      int
	callback_storage *base.PoisonCallbackStorage
}

func NewPgDecryptor(client_id []byte, decryptor base.DataDecryptor) *PgDecryptor {
	return &PgDecryptor{
		is_with_zone:     false,
		pg_decryptor:     decryptor,
		binary_decryptor: binary.NewBinaryDecryptor(),
		client_id:        client_id,
		// longest tag (escape) + bin
		match_buffer: make([]byte, len(ESCAPE_TAG_BEGIN)+len(base.TAG_BEGIN)),
		match_index:  0,
	}
}

func (decryptor *PgDecryptor) SetPoisonKey(key []byte) {
	decryptor.poison_key = key
}

func (decryptor *PgDecryptor) GetPoisonKey() []byte {
	return decryptor.poison_key
}

func (decryptor *PgDecryptor) SetWithZone(b bool) {
	decryptor.is_with_zone = b
}

func (decryptor *PgDecryptor) SetZoneMatcher(zone_matcher *zone.ZoneIdMatcher) {
	decryptor.zone_matcher = zone_matcher
}

func (decryptor *PgDecryptor) IsMatchedZone() bool {
	return decryptor.zone_matcher.IsMatched() && decryptor.key_store.HasZonePrivateKey(decryptor.zone_matcher.GetZoneId())
}

func (decryptor *PgDecryptor) MatchZone(b byte) bool {
	return decryptor.zone_matcher.Match(b)
}

func (decryptor *PgDecryptor) GetMatchedZoneId() []byte {
	if decryptor.IsWithZone() {
		return decryptor.zone_matcher.GetZoneId()
	} else {
		return nil
	}
}

func (decryptor *PgDecryptor) ResetZoneMatch() {
	if decryptor.zone_matcher != nil {
		decryptor.zone_matcher.Reset()
	}
}

func (decryptor *PgDecryptor) MatchBeginTag(char byte) bool {
	/* should be called two decryptors */
	matched := decryptor.pg_decryptor.MatchBeginTag(char)
	matched = decryptor.binary_decryptor.MatchBeginTag(char) || matched
	if matched {
		decryptor.match_buffer[decryptor.match_index] = char
		decryptor.match_index++
	}
	return matched
}

func (decryptor *PgDecryptor) IsWithZone() bool {
	return decryptor.is_with_zone
}

func (decryptor *PgDecryptor) IsMatched() bool {
	if decryptor.binary_decryptor.IsMatched() {
		log.Println("Debug: matched binary decryptor")
		decryptor.matched_decryptor = decryptor.binary_decryptor
		return true
	} else if decryptor.pg_decryptor.IsMatched() {
		log.Println("Debug: matched pg decryptor")
		decryptor.matched_decryptor = decryptor.pg_decryptor
		return true
	} else {
		decryptor.matched_decryptor = nil
		return false
	}
}
func (decryptor *PgDecryptor) Reset() {
	decryptor.matched_decryptor = nil
	decryptor.binary_decryptor.Reset()
	decryptor.pg_decryptor.Reset()
	decryptor.match_index = 0
}
func (decryptor *PgDecryptor) GetMatched() []byte {
	return decryptor.match_buffer[:decryptor.match_index]
}
func (decryptor *PgDecryptor) ReadSymmetricKey(private_key *keys.PrivateKey, reader io.Reader) ([]byte, []byte, error) {
	return decryptor.matched_decryptor.ReadSymmetricKey(private_key, reader)
}

func (decryptor *PgDecryptor) ReadData(symmetric_key, zone_id []byte, reader io.Reader) ([]byte, error) {
	/* due to using two decryptors can be case when one decryptor match 2 bytes
	from TAG_BEGIN then didn't match anymore but another decryptor matched at
	this time and was successfully used for decryption, we need return 2 bytes
	matched and buffered by first decryptor and decrypted data from the second

	for example case of matching begin tag:
	BEGIN_TA - failed decryptor1
	00BEGIN_TAG - successfull decryptor2
	in this case first decryptor1 matched not full begin_tag and failed on 'G' but
	at this time was matched decryptor2 and successfully matched next bytes and decrypted data
	so we need return diff of two matches 'BE' and decrypted data
	*/

	// take length of fully matched tag begin (each decryptor match tag begin with different length)
	correct_match_begin_tag_length := len(decryptor.matched_decryptor.GetMatched())
	// take diff count of matched between two decryptors
	false_buffered_begin_tag_length := decryptor.match_index - correct_match_begin_tag_length
	if false_buffered_begin_tag_length > 0 {
		log.Printf("Debug: return with false matched %v bytes\n", false_buffered_begin_tag_length)
		decrypted, err := decryptor.matched_decryptor.ReadData(symmetric_key, zone_id, reader)
		return append(decryptor.match_buffer[:false_buffered_begin_tag_length], decrypted...), err
	} else {
		return decryptor.matched_decryptor.ReadData(symmetric_key, zone_id, reader)
	}
}

func (decryptor *PgDecryptor) SetKeyStore(store keystore.KeyStore) {
	decryptor.key_store = store
}

func (decryptor *PgDecryptor) GetPrivateKey() (*keys.PrivateKey, error) {
	if decryptor.IsWithZone() {
		return decryptor.key_store.GetZonePrivateKey(decryptor.GetMatchedZoneId())
	} else {
		return decryptor.key_store.GetServerPrivateKey(decryptor.client_id)
	}
}

func (decryptor *PgDecryptor) GetPoisonCallbackStorage() *base.PoisonCallbackStorage {
	return decryptor.callback_storage
}

func (decryptor *PgDecryptor) SetPoisonCallbackStorage(storage *base.PoisonCallbackStorage) {
	decryptor.callback_storage = storage
}