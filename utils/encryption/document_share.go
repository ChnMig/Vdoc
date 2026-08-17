package encryption

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
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

func GenerateDocumentShareCapability(shareID string, keyring Keyring) (string, DocumentShareCapabilityRecord, error) {
	if !documentShareIDPattern.MatchString(shareID) {
		return "", DocumentShareCapabilityRecord{}, ErrInvalidDocumentShareCapability
	}
	randomHex, err := random.Hex(24)
	if err != nil {
		return "", DocumentShareCapabilityRecord{}, ErrInvalidDocumentShareCapability
	}
	secret := "vdoc_share_" + randomHex

	record, err := encryptDocumentShareCapability(shareID, secret, keyring)
	if err != nil {
		return "", DocumentShareCapabilityRecord{}, ErrInvalidDocumentShareCapability
	}
	return secret, record, nil
}

func RevealDocumentShareCapability(shareID string, keyring Keyring, record DocumentShareCapabilityRecord) (string, error) {
	if !documentShareIDPattern.MatchString(shareID) ||
		record.KID == "" ||
		!documentShareCapabilityHashPattern.MatchString(record.Hash) {
		return "", ErrInvalidDocumentShareCapability
	}

	keyMaterial, err := keyring.keyFor(record.KID)
	if err != nil {
		return "", ErrInvalidDocumentShareCapability
	}
	plaintext, err := decryptAESGCM(record.Ciphertext, keyMaterial, documentShareAAD(shareID, record.KID))
	if err != nil {
		return "", ErrInvalidDocumentShareCapability
	}
	secret := string(plaintext)
	if !VerifyDocumentShareCapability(secret, record.Hash) {
		return "", ErrInvalidDocumentShareCapability
	}
	return secret, nil
}

func ReencryptDocumentShareCapability(shareID string, keyring Keyring, record DocumentShareCapabilityRecord) (DocumentShareCapabilityRecord, error) {
	secret, err := RevealDocumentShareCapability(shareID, keyring, record)
	if err != nil {
		return DocumentShareCapabilityRecord{}, err
	}
	return encryptDocumentShareCapability(shareID, secret, keyring)
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

func encryptDocumentShareCapability(shareID, secret string, keyring Keyring) (DocumentShareCapabilityRecord, error) {
	kid := keyring.ActiveKID()
	keyMaterial, err := keyring.keyFor(kid)
	if err != nil {
		return DocumentShareCapabilityRecord{}, err
	}
	ciphertext, _, err := encryptAESGCM([]byte(secret), keyMaterial, documentShareAAD(shareID, kid), kid)
	if err != nil {
		return DocumentShareCapabilityRecord{}, err
	}
	return DocumentShareCapabilityRecord{
		Hash:       hashDocumentShareCapability(secret),
		Ciphertext: ciphertext,
		KID:        kid,
	}, nil
}

func documentShareAAD(shareID, kid string) []byte {
	return []byte("vdoc.document-share\x00" + kid + "\x00" + shareID)
}

func hashDocumentShareCapability(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
