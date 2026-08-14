package authentication

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"vdoc/config"
)

const (
	DocumentShareUnlockProofPrefix = "vdoc_share_unlock_"
	documentShareUnlockProofType   = "VdocShareUnlock"
	documentShareUnlockProofKID    = "document-share-unlock-v1"
	documentShareUnlockKeyLabel    = "vdoc.document-share.unlock-proof.v1"
	documentShareUnlockLifetime    = int64(900)
)

var (
	ErrInvalidDocumentShareUnlockProof = errors.New("invalid document share unlock proof")
	documentShareUnlockProofPattern    = regexp.MustCompile(`^vdoc_share_unlock_[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
	documentShareUnlockIDPattern       = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type documentShareUnlockHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	KID       string `json:"kid"`
}

type documentShareUnlockClaims struct {
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func SignDocumentShareUnlockProof(shareID string, now time.Time, shareExpiresAt *time.Time) (string, time.Time, error) {
	if !documentShareUnlockIDPattern.MatchString(shareID) {
		return "", time.Time{}, ErrInvalidDocumentShareUnlockProof
	}
	issuedAt := now.Unix()
	expiresAt := issuedAt + documentShareUnlockLifetime
	if shareExpiresAt != nil && shareExpiresAt.Unix() < expiresAt {
		expiresAt = shareExpiresAt.Unix()
	}
	if expiresAt <= issuedAt {
		return "", time.Time{}, ErrInvalidDocumentShareUnlockProof
	}

	headerJSON, err := json.Marshal(documentShareUnlockHeader{
		Algorithm: "HS256",
		Type:      documentShareUnlockProofType,
		KID:       documentShareUnlockProofKID,
	})
	if err != nil {
		return "", time.Time{}, ErrInvalidDocumentShareUnlockProof
	}
	claimsJSON, err := json.Marshal(documentShareUnlockClaims{
		Subject:   shareID,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", time.Time{}, ErrInvalidDocumentShareUnlockProof
	}
	headerSegment := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsSegment := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := headerSegment + "." + claimsSegment
	signature := signDocumentShareUnlock(signingInput, config.JWTKey)
	proof := DocumentShareUnlockProofPrefix + signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
	return proof, time.Unix(expiresAt, 0).UTC(), nil
}

func ValidateDocumentShareUnlockProof(proof, shareID string, now time.Time) error {
	if !documentShareUnlockIDPattern.MatchString(shareID) || !documentShareUnlockProofPattern.MatchString(proof) {
		return ErrInvalidDocumentShareUnlockProof
	}
	compact := strings.TrimPrefix(proof, DocumentShareUnlockProofPrefix)
	segments := strings.Split(compact, ".")
	if len(segments) != 3 {
		return ErrInvalidDocumentShareUnlockProof
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(segments[0])
	if err != nil {
		return ErrInvalidDocumentShareUnlockProof
	}
	header, err := parseDocumentShareUnlockHeader(headerJSON)
	if err != nil || header.Algorithm != "HS256" || header.Type != documentShareUnlockProofType || header.KID != documentShareUnlockProofKID {
		return ErrInvalidDocumentShareUnlockProof
	}
	signature, err := base64.RawURLEncoding.DecodeString(segments[2])
	if err != nil || !hmac.Equal(signature, signDocumentShareUnlock(segments[0]+"."+segments[1], config.JWTKey)) {
		return ErrInvalidDocumentShareUnlockProof
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return ErrInvalidDocumentShareUnlockProof
	}
	claims, err := parseDocumentShareUnlockClaims(claimsJSON)
	if err != nil || claims.Subject != shareID || !documentShareUnlockIDPattern.MatchString(claims.Subject) {
		return ErrInvalidDocumentShareUnlockProof
	}
	nowUnix := now.Unix()
	lifetime := claims.ExpiresAt - claims.IssuedAt
	if claims.IssuedAt > nowUnix || nowUnix >= claims.ExpiresAt ||
		claims.ExpiresAt <= claims.IssuedAt || lifetime <= 0 || lifetime > documentShareUnlockLifetime {
		return ErrInvalidDocumentShareUnlockProof
	}
	return nil
}

func signDocumentShareUnlock(signingInput, jwtKey string) []byte {
	keyMAC := hmac.New(sha256.New, []byte(jwtKey))
	keyMAC.Write([]byte(documentShareUnlockKeyLabel))
	mac := hmac.New(sha256.New, keyMAC.Sum(nil))
	mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}

func parseDocumentShareUnlockHeader(data []byte) (documentShareUnlockHeader, error) {
	var header documentShareUnlockHeader
	err := decodeExactJSONObject(data, func(key string, decoder *json.Decoder) error {
		switch key {
		case "alg":
			return decoder.Decode(&header.Algorithm)
		case "typ":
			return decoder.Decode(&header.Type)
		case "kid":
			return decoder.Decode(&header.KID)
		default:
			return ErrInvalidDocumentShareUnlockProof
		}
	})
	if err != nil {
		return documentShareUnlockHeader{}, ErrInvalidDocumentShareUnlockProof
	}
	return header, nil
}

func parseDocumentShareUnlockClaims(data []byte) (documentShareUnlockClaims, error) {
	var claims documentShareUnlockClaims
	err := decodeExactJSONObject(data, func(key string, decoder *json.Decoder) error {
		switch key {
		case "sub":
			return decoder.Decode(&claims.Subject)
		case "iat":
			value, err := decodeDocumentShareUnlockInteger(decoder)
			claims.IssuedAt = value
			return err
		case "exp":
			value, err := decodeDocumentShareUnlockInteger(decoder)
			claims.ExpiresAt = value
			return err
		default:
			return ErrInvalidDocumentShareUnlockProof
		}
	})
	if err != nil {
		return documentShareUnlockClaims{}, ErrInvalidDocumentShareUnlockProof
	}
	return claims, nil
}

func decodeExactJSONObject(data []byte, decodeField func(string, *json.Decoder) error) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return ErrInvalidDocumentShareUnlockProof
	}
	seen := make(map[string]struct{}, 3)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return ErrInvalidDocumentShareUnlockProof
		}
		key, ok := keyToken.(string)
		if !ok {
			return ErrInvalidDocumentShareUnlockProof
		}
		if _, exists := seen[key]; exists {
			return ErrInvalidDocumentShareUnlockProof
		}
		seen[key] = struct{}{}
		if err := decodeField(key, decoder); err != nil {
			return ErrInvalidDocumentShareUnlockProof
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != 3 {
		return ErrInvalidDocumentShareUnlockProof
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrInvalidDocumentShareUnlockProof
	}
	return nil
}

func decodeDocumentShareUnlockInteger(decoder *json.Decoder) (int64, error) {
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return 0, ErrInvalidDocumentShareUnlockProof
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, ErrInvalidDocumentShareUnlockProof
	}
	return value, nil
}
