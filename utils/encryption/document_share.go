package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"regexp"

	"vdoc/utils/random"
)

const DocumentShareCipherKID = "document-share-aes-gcm-v1"

var (
	ErrInvalidDocumentShareCapability  = errors.New("invalid document share capability")
	documentShareIDPattern             = regexp.MustCompile(`^[0-9a-f]{32}$`)
	documentShareCapabilityPattern     = regexp.MustCompile(`^vdoc_share_[0-9a-f]{48}$`)
	documentShareCapabilityHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type DocumentShareCapabilityRecord struct {
	Hash       string
	Ciphertext []byte
	KID        string
}

func GenerateDocumentShareCapability(shareID, keyMaterial string) (string, DocumentShareCapabilityRecord, error) {
	if !documentShareIDPattern.MatchString(shareID) {
		return "", DocumentShareCapabilityRecord{}, ErrInvalidDocumentShareCapability
	}
	randomHex, err := random.Hex(24)
	if err != nil {
		return "", DocumentShareCapabilityRecord{}, ErrInvalidDocumentShareCapability
	}
	secret := "vdoc_share_" + randomHex

	block, err := aes.NewCipher(deriveMCPTokenKey(keyMaterial))
	if err != nil {
		return "", DocumentShareCapabilityRecord{}, ErrInvalidDocumentShareCapability
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", DocumentShareCapabilityRecord{}, ErrInvalidDocumentShareCapability
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", DocumentShareCapabilityRecord{}, ErrInvalidDocumentShareCapability
	}
	ciphertext := gcm.Seal(append([]byte(nil), nonce...), nonce, []byte(secret), documentShareAAD(shareID))

	return secret, DocumentShareCapabilityRecord{
		Hash:       hashDocumentShareCapability(secret),
		Ciphertext: ciphertext,
		KID:        DocumentShareCipherKID,
	}, nil
}

func RevealDocumentShareCapability(shareID, keyMaterial string, record DocumentShareCapabilityRecord) (string, error) {
	if !documentShareIDPattern.MatchString(shareID) ||
		record.KID != DocumentShareCipherKID ||
		!documentShareCapabilityHashPattern.MatchString(record.Hash) {
		return "", ErrInvalidDocumentShareCapability
	}

	block, err := aes.NewCipher(deriveMCPTokenKey(keyMaterial))
	if err != nil {
		return "", ErrInvalidDocumentShareCapability
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(record.Ciphertext) < gcm.NonceSize() {
		return "", ErrInvalidDocumentShareCapability
	}
	nonce := record.Ciphertext[:gcm.NonceSize()]
	sealed := record.Ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, sealed, documentShareAAD(shareID))
	if err != nil {
		return "", ErrInvalidDocumentShareCapability
	}
	secret := string(plaintext)
	if !VerifyDocumentShareCapability(secret, record.Hash) {
		return "", ErrInvalidDocumentShareCapability
	}
	return secret, nil
}

func VerifyDocumentShareCapability(secret, storedHash string) bool {
	if !documentShareCapabilityPattern.MatchString(secret) ||
		!documentShareCapabilityHashPattern.MatchString(storedHash) {
		return false
	}
	expected, err := hex.DecodeString(storedHash)
	if err != nil {
		return false
	}
	actual := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(actual[:], expected) == 1
}

func documentShareAAD(shareID string) []byte {
	return []byte("vdoc.document-share\x00" + DocumentShareCipherKID + "\x00" + shareID)
}

func hashDocumentShareCapability(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
