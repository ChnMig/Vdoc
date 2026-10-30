package vdoc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
	"vdoc/utils/encryption"
	"vdoc/utils/id"
)

type repositoryStoredSubscription struct {
	documentrepo.Subscription
	SecretCiphertext []byte `json:"secret_ciphertext,omitempty"`
	CipherKID        string `json:"cipher_kid,omitempty"`
}

type repositoryStoredDelivery struct {
	documentrepo.Delivery
	Event          documentrepo.Event `json:"event"`
	LeaseID        string             `json:"lease_id,omitempty"`
	LeaseExpiresAt *time.Time         `json:"lease_expires_at,omitempty"`
}

type repositoryAutomationRunner struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var repositoryAutomationRunners sync.Map

type repositoryLatestSequenceStorage interface {
	RepositoryLatestSequence(context.Context, string, string) (int64, error)
}

func (s *Store) RepositorySubscriptions(actorID, projectID string) ([]documentrepo.Subscription, error) {
	values := []documentrepo.Subscription{}
	err := s.WithRepositoryWrite(actorID, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		records, err := storage.RepositoryRecords(s.requestContext(), projectID, "subscription", 0, 100)
		if err != nil {
			return err
		}
		for _, record := range records {
			var value repositoryStoredSubscription
			if err := json.Unmarshal(record.Payload, &value); err != nil {
				return err
			}
			values = append(values, value.Subscription)
		}
		return nil
	})
	return values, err
}

func (s *Store) RepositoryCreateSubscription(actorID, projectID string, input documentrepo.SubscriptionInput) (*documentrepo.CreatedSubscription, error) {
	input.Name, input.URL = strings.TrimSpace(input.Name), strings.TrimSpace(input.URL)
	if input.Name == "" || len(input.Name) > 200 || (input.Kind != "in_app" && input.Kind != "webhook") || len(input.EventTypes) > 20 {
		return nil, ErrInvalidArgument
	}
	if len(input.EventTypes) == 0 {
		input.EventTypes = []string{"release.published"}
	}
	for _, eventType := range input.EventTypes {
		if len(eventType) > 100 || eventType == "" || strings.ContainsAny(eventType, " \t\r\n") {
			return nil, ErrInvalidArgument
		}
	}
	slices.Sort(input.EventTypes)
	input.EventTypes = slices.Compact(input.EventTypes)
	if input.Kind == "webhook" {
		var err error
		input.URL, err = normalizeRepositoryWebhookURL(s.requestContext(), input.URL)
		if err != nil {
			return nil, err
		}
	} else if input.URL != "" {
		return nil, ErrInvalidArgument
	}
	value := repositoryStoredSubscription{Subscription: documentrepo.Subscription{ID: id.GenerateID(), ProjectID: projectID, Name: input.Name, Kind: input.Kind, EventTypes: input.EventTypes, BranchID: input.BranchID, DocumentID: input.DocumentID, URL: input.URL, Generation: 1, CreatedBy: actorID, CreatedAt: time.Now().UTC()}}
	secret := ""
	if input.Kind == "webhook" {
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		secret = hex.EncodeToString(random[:])
		var err error
		value.SecretCiphertext, value.CipherKID, err = encryption.EncryptMCPToken(repositorySubscriptionSecretBinding(projectID, value.ID, secret), s.cipherKeyring)
		if err != nil {
			return nil, err
		}
	}
	err := s.WithRepositoryWrite(actorID, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		records, err := storage.RepositoryRecords(s.requestContext(), projectID, "subscription", 0, 101)
		if err != nil {
			return err
		}
		if len(records) >= 100 {
			return fmt.Errorf("%w: project subscription limit reached", ErrFailedPrecondition)
		}
		if input.BranchID != "" {
			if record, err := storage.RepositoryRecord(s.requestContext(), projectID, "branch", input.BranchID); err != nil {
				return err
			} else if record == nil {
				return ErrNotFound
			}
		}
		if latest, ok := storage.(repositoryLatestSequenceStorage); ok {
			value.LastSequence, err = latest.RepositoryLatestSequence(s.requestContext(), projectID, "event")
		} else {
			value.LastSequence, err = repositoryLatestEventSequence(s.requestContext(), storage, projectID)
		}
		if err != nil {
			return err
		}
		return putRepositoryAutomation(s.requestContext(), storage, projectID, "subscription", value.ID, value, 1, nil)
	})
	if err != nil {
		return nil, err
	}
	return &documentrepo.CreatedSubscription{Subscription: value.Subscription, Secret: secret}, nil
}

func repositoryLatestEventSequence(ctx context.Context, storage documentrepo.Storage, projectID string) (int64, error) {
	var after int64
	for {
		records, err := storage.RepositoryRecords(ctx, projectID, "event", after, 100)
		if err != nil {
			return 0, err
		}
		for _, record := range records {
			if record.Sequence > after {
				after = record.Sequence
			}
		}
		if len(records) < 100 {
			return after, nil
		}
	}
}

func (s *Store) RepositoryPauseSubscription(actorID, projectID, subscriptionID string, paused bool) (*documentrepo.Subscription, error) {
	var value repositoryStoredSubscription
	err := s.WithRepositoryWrite(actorID, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		record, err := getRepositoryAutomation(s.requestContext(), storage, projectID, "subscription", subscriptionID, &value)
		if err != nil {
			return err
		}
		value.Paused, value.Generation = paused, record.Generation+1
		return putRepositoryAutomation(s.requestContext(), storage, projectID, "subscription", value.ID, value, value.Generation, &record.Generation)
	})
	return &value.Subscription, err
}

func (s *Store) RepositoryDeliveries(actorID, projectID string, after int64) ([]documentrepo.Delivery, error) {
	if after < 0 {
		return nil, ErrInvalidArgument
	}
	values := []documentrepo.Delivery{}
	err := s.WithRepositoryWrite(actorID, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		records, err := storage.RepositoryRecords(s.requestContext(), projectID, "delivery", after, 100)
		if err != nil {
			return err
		}
		for _, record := range records {
			var value repositoryStoredDelivery
			if err := json.Unmarshal(record.Payload, &value); err != nil {
				return err
			}
			value.Sequence = record.Sequence
			values = append(values, value.Delivery)
		}
		return nil
	})
	return values, err
}

func (s *Store) RepositoryRetryDelivery(actorID, projectID, deliveryID string) (*documentrepo.Delivery, error) {
	var value repositoryStoredDelivery
	err := s.WithRepositoryWrite(actorID, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		record, err := getRepositoryAutomation(s.requestContext(), storage, projectID, "delivery", deliveryID, &value)
		if err != nil {
			return err
		}
		if value.Status != "failed" {
			return ErrFailedPrecondition
		}
		value.Status, value.LastError, value.NextAttemptAt, value.CompletedAt = "pending", "", nil, nil
		value.AttemptCount = 0
		value.Sequence = record.Sequence
		return putRepositoryAutomation(s.requestContext(), storage, projectID, "delivery", value.ID, value, record.Generation+1, &record.Generation)
	})
	return &value.Delivery, err
}

// InitRepositoryAutomation 在启动时验证并旋转订阅密钥，然后启动独立投递循环。
func (s *Store) InitRepositoryAutomation() error {
	if err := s.rotateRepositorySubscriptionSecrets(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := &repositoryAutomationRunner{cancel: cancel, done: make(chan struct{})}
	if _, loaded := repositoryAutomationRunners.LoadOrStore(s.storeState, runner); loaded {
		cancel()
		return nil
	}
	go func() {
		defer close(runner.done)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.WithContext(ctx).repositoryDrainProjects()
			}
		}
	}()
	return nil
}

func (s *Store) StopRepositoryAutomation() {
	if value, ok := repositoryAutomationRunners.LoadAndDelete(s.storeState); ok {
		runner := value.(*repositoryAutomationRunner)
		runner.cancel()
		<-runner.done
	}
}

func (s *Store) repositoryDrainProjects() {
	s.mu.Lock()
	if err := s.refreshLocked(); err != nil {
		s.mu.Unlock()
		return
	}
	actors := map[string]string{}
	projectIDs := make([]string, 0, len(s.projects))
	for projectID, project := range s.projects {
		projectIDs = append(projectIDs, projectID)
		if project.Status != ProjectStatusActive {
			continue
		}
		for userID := range s.users {
			if s.canManageProjectLocked(userID, projectID) {
				actors[projectID] = userID
				break
			}
		}
	}
	s.mu.Unlock()
	for _, projectID := range projectIDs {
		if s.requestContext().Err() != nil {
			return
		}
		_ = s.repositoryExpireAnalyses(projectID)
		if actorID := actors[projectID]; actorID != "" {
			_ = s.RepositoryDrainDeliveries(actorID, projectID)
		}
	}
}

// RepositoryDrainDeliveries 只推进投递账本；任何网络失败都不会回滚原提交。
func (s *Store) RepositoryDrainDeliveries(actorID, projectID string) error {
	ctx, cancel := context.WithTimeout(s.requestContext(), 30*time.Second)
	defer cancel()
	s = s.WithContext(ctx)
	if err := s.repositoryEnqueueDeliveries(actorID, projectID); err != nil {
		return err
	}
	var after int64
	attempted := 0
	for {
		var records []documentrepo.Record
		err := s.WithRepositoryWrite(actorID, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
			var err error
			records, err = storage.RepositoryRecords(s.requestContext(), projectID, "delivery", after, 100)
			return err
		})
		if err != nil {
			return err
		}
		for _, record := range records {
			after = record.Sequence
			var delivery repositoryStoredDelivery
			if err := json.Unmarshal(record.Payload, &delivery); err != nil {
				return err
			}
			if !repositoryDeliveryDue(delivery, time.Now()) {
				continue
			}
			if err := s.repositorySendDelivery(actorID, projectID, delivery.ID); err != nil {
				if errors.Is(err, ErrFailedPrecondition) || errors.Is(err, ErrPermissionDenied) {
					continue
				}
				return err
			}
			attempted++
			if attempted == 32 {
				return nil
			}
		}
		if len(records) < 100 {
			return nil
		}
	}
}

func (s *Store) repositoryEnqueueDeliveries(actorID, projectID string) error {
	return s.WithRepositoryWrite(actorID, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		subscriptions, err := storage.RepositoryRecords(s.requestContext(), projectID, "subscription", 0, 100)
		if err != nil {
			return err
		}
		for _, record := range subscriptions {
			var subscription repositoryStoredSubscription
			if err := json.Unmarshal(record.Payload, &subscription); err != nil {
				return err
			}
			if subscription.Paused {
				continue
			}
			events, err := storage.RepositoryRecords(s.requestContext(), projectID, "event", subscription.LastSequence, 100)
			if err != nil {
				return err
			}
			for _, eventRecord := range events {
				var event documentrepo.Event
				if err := json.Unmarshal(eventRecord.Payload, &event); err != nil {
					return err
				}
				event.Sequence = eventRecord.Sequence
				matches, err := repositorySubscriptionMatches(s.requestContext(), storage, subscription.Subscription, event)
				if err != nil {
					return err
				}
				if matches {
					deliveryID := sha(subscription.ID + ":" + event.ID)
					existing, err := storage.RepositoryRecord(s.requestContext(), projectID, "delivery", deliveryID)
					if err != nil && !errors.Is(err, ErrNotFound) {
						return err
					}
					if existing == nil {
						value := repositoryStoredDelivery{Delivery: documentrepo.Delivery{ID: deliveryID, SubscriptionID: subscription.ID, EventID: event.ID, EventSequence: event.Sequence, EventType: event.EventType, Status: "pending", CreatedAt: time.Now().UTC()}, Event: event}
						if err := putRepositoryAutomation(s.requestContext(), storage, projectID, "delivery", deliveryID, value, 1, nil); err != nil {
							return err
						}
					}
				}
				subscription.LastSequence = eventRecord.Sequence
			}
			if len(events) > 0 {
				subscription.Generation = record.Generation + 1
				if err := putRepositoryAutomation(s.requestContext(), storage, projectID, "subscription", subscription.ID, subscription, subscription.Generation, &record.Generation); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func repositorySubscriptionMatches(ctx context.Context, storage documentrepo.Storage, subscription documentrepo.Subscription, event documentrepo.Event) (bool, error) {
	if subscription.BranchID != "" && subscription.BranchID != event.BranchID {
		return false, nil
	}
	if !slices.Contains(subscription.EventTypes, "*") && !slices.Contains(subscription.EventTypes, event.EventType) {
		return false, nil
	}
	if subscription.DocumentID == "" {
		return true, nil
	}
	files := [2]*documentrepo.File{}
	for i, commitID := range []string{event.BeforeCommitID, event.AfterCommitID} {
		if commitID == "" {
			continue
		}
		var commit documentrepo.Commit
		if _, err := getRepositoryAutomation(ctx, storage, subscription.ProjectID, "commit", commitID, &commit); err != nil {
			return false, err
		}
		for _, file := range commit.Files {
			if file.DocumentID == subscription.DocumentID {
				copy := file
				files[i] = &copy
				break
			}
		}
	}
	if files[0] == nil && files[1] == nil {
		return false, nil
	}
	if event.EventType == "commit.created" || event.EventType == "branch.updated" {
		return !reflect.DeepEqual(files[0], files[1]), nil
	}
	return true, nil
}

func repositoryDeliveryDue(value repositoryStoredDelivery, now time.Time) bool {
	if value.Status == "succeeded" || value.Status == "failed" {
		return false
	}
	if value.NextAttemptAt != nil && value.NextAttemptAt.After(now) {
		return false
	}
	return value.Status != "processing" || value.LeaseExpiresAt == nil || !value.LeaseExpiresAt.After(now)
}

func (s *Store) repositorySendDelivery(drainingActor, projectID, deliveryID string) error {
	var delivery repositoryStoredDelivery
	var subscription repositoryStoredSubscription
	// 由当前管理员读取账本，再以配置者身份检查其仍有管理权限。
	err := s.WithRepositoryWrite(drainingActor, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		if _, err := getRepositoryAutomation(s.requestContext(), storage, projectID, "delivery", deliveryID, &delivery); err != nil {
			return err
		}
		_, err := getRepositoryAutomation(s.requestContext(), storage, projectID, "subscription", delivery.SubscriptionID, &subscription)
		return err
	})
	if err != nil {
		return err
	}
	err = s.repositorySendDeliveryAs(subscription.CreatedBy, projectID, deliveryID)
	if !errors.Is(err, ErrPermissionDenied) && !errors.Is(err, ErrUnauthenticated) {
		return err
	}
	return s.WithRepositoryWrite(drainingActor, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		subscriptionRecord, err := getRepositoryAutomation(s.requestContext(), storage, projectID, "subscription", subscription.ID, &subscription)
		if err != nil {
			return err
		}
		subscription.Paused, subscription.Generation = true, subscriptionRecord.Generation+1
		if err := putRepositoryAutomation(s.requestContext(), storage, projectID, "subscription", subscription.ID, subscription, subscription.Generation, &subscriptionRecord.Generation); err != nil {
			return err
		}
		record, err := getRepositoryAutomation(s.requestContext(), storage, projectID, "delivery", deliveryID, &delivery)
		if err != nil || delivery.Status == "succeeded" {
			return err
		}
		now := time.Now().UTC()
		delivery.Status, delivery.LastError, delivery.CompletedAt = "failed", "Subscription owner's project administration access is no longer active.", &now
		delivery.LeaseID, delivery.LeaseExpiresAt, delivery.NextAttemptAt = "", nil, nil
		return putRepositoryAutomation(s.requestContext(), storage, projectID, "delivery", delivery.ID, delivery, record.Generation+1, &record.Generation)
	})
}

func (s *Store) repositorySendDeliveryAs(actorID, projectID, deliveryID string) error {
	var delivery repositoryStoredDelivery
	var subscription repositoryStoredSubscription
	var generation int64
	err := s.WithRepositoryWrite(actorID, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		record, err := getRepositoryAutomation(s.requestContext(), storage, projectID, "delivery", deliveryID, &delivery)
		if err != nil {
			return err
		}
		if _, err := getRepositoryAutomation(s.requestContext(), storage, projectID, "subscription", delivery.SubscriptionID, &subscription); err != nil {
			return err
		}
		now := time.Now().UTC()
		if subscription.Paused || subscription.CreatedBy != actorID || !repositoryDeliveryDue(delivery, now) {
			return ErrFailedPrecondition
		}
		leaseEnd := now.Add(2 * time.Minute)
		delivery.Status, delivery.LeaseID, delivery.LeaseExpiresAt = "processing", id.GenerateID(), &leaseEnd
		delivery.AttemptCount++
		delivery.AttemptedAt, delivery.NextAttemptAt = &now, nil
		generation = record.Generation + 1
		return putRepositoryAutomation(s.requestContext(), storage, projectID, "delivery", delivery.ID, delivery, generation, &record.Generation)
	})
	if err != nil {
		return err
	}
	httpStatus, retry, sendErr := 0, false, error(nil)
	if subscription.Kind == "webhook" {
		secret, err := s.repositorySubscriptionSecret(projectID, subscription)
		if err != nil {
			sendErr = errors.New("Webhook signing key is unavailable.")
		} else {
			ctx, cancel := context.WithTimeout(s.requestContext(), 15*time.Second)
			httpStatus, retry, sendErr = sendRepositoryWebhook(ctx, newAIHTTPClient(), subscription.URL, secret, delivery.Event)
			cancel()
		}
	}
	now := time.Now().UTC()
	delivery.HTTPStatus, delivery.LeaseExpiresAt = httpStatus, nil
	if sendErr == nil {
		delivery.Status, delivery.LastError, delivery.CompletedAt = "succeeded", "", &now
	} else {
		delivery.LastError = sendErr.Error()
		if retry && delivery.AttemptCount < 8 {
			next := now.Add(repositoryDeliveryBackoff(delivery.AttemptCount))
			delivery.Status, delivery.NextAttemptAt = "pending", &next
		} else {
			delivery.Status, delivery.CompletedAt = "failed", &now
		}
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(s.requestContext()), 10*time.Second)
	defer cancel()
	return s.WithContext(finishCtx).WithRepositoryWrite(actorID, projectID, MemberRoleAdmin, func(storage documentrepo.Storage) error {
		var current repositoryStoredDelivery
		record, err := getRepositoryAutomation(finishCtx, storage, projectID, "delivery", delivery.ID, &current)
		if err != nil {
			return err
		}
		if current.LeaseID != delivery.LeaseID || record.Generation != generation {
			return ErrFailedPrecondition
		}
		delivery.LeaseID = ""
		return putRepositoryAutomation(finishCtx, storage, projectID, "delivery", delivery.ID, delivery, generation+1, &generation)
	})
}

func repositoryDeliveryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 {
		attempt = 10
	}
	return min(time.Hour, 5*time.Second*time.Duration(1<<uint(attempt-1)))
}

func normalizeRepositoryWebhookURL(ctx context.Context, raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: webhook requires a public HTTPS URL without user credentials or a fragment", ErrInvalidArgument)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || strings.ContainsAny(host, " \t\r\n%") || isUnsafeAIProviderHost(host) {
		return "", fmt.Errorf("%w: unsafe webhook host", ErrInvalidArgument)
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return "", fmt.Errorf("%w: webhook host cannot be resolved", ErrInvalidArgument)
	}
	if err := validateAIProviderResolvedAddrs(host, addresses); err != nil {
		return "", fmt.Errorf("%w: unsafe webhook destination", ErrInvalidArgument)
	}
	return parsed.String(), nil
}

func sendRepositoryWebhook(ctx context.Context, client *http.Client, target, secret string, event documentrepo.Event) (int, bool, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return 0, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, false, errors.New("Invalid webhook request.")
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Vdoc-Webhook/1")
	req.Header.Set("X-Vdoc-Event-ID", event.ID)
	req.Header.Set("X-Vdoc-Timestamp", timestamp)
	req.Header.Set("X-Vdoc-Signature", repositoryWebhookSignature(secret, timestamp, body))
	safeClient := *client
	safeClient.CheckRedirect = rejectAIProviderRedirect
	response, err := safeClient.Do(req)
	if err != nil {
		return 0, true, errors.New("Webhook connection failed or timed out.")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response.StatusCode, false, nil
	}
	retry := response.StatusCode == 408 || response.StatusCode == 429 || response.StatusCode >= 500
	return response.StatusCode, retry, fmt.Errorf("Webhook returned HTTP %d.", response.StatusCode)
}

func repositoryWebhookSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func repositorySubscriptionSecretBinding(projectID, subscriptionID, secret string) string {
	return projectID + ":" + subscriptionID + ":" + secret
}

func (s *Store) repositorySubscriptionSecret(projectID string, subscription repositoryStoredSubscription) (string, error) {
	plain, err := encryption.DecryptMCPToken(subscription.SecretCiphertext, s.cipherKeyring, subscription.CipherKID)
	if err != nil {
		return "", err
	}
	prefix := repositorySubscriptionSecretBinding(projectID, subscription.ID, "")
	if !strings.HasPrefix(plain, prefix) || len(strings.TrimPrefix(plain, prefix)) != 64 {
		return "", ErrFailedPrecondition
	}
	return strings.TrimPrefix(plain, prefix), nil
}

func (s *Store) rotateRepositorySubscriptionSecrets() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return err
	}
	if s.persistence == nil {
		return nil
	}
	rotate := func(repository domain.Repository) error {
		storage, ok := repository.(documentrepo.Storage)
		if !ok {
			return nil
		}
		projectIDs := make([]string, 0, len(s.projects))
		for projectID := range s.projects {
			projectIDs = append(projectIDs, projectID)
		}
		slices.Sort(projectIDs)
		for _, projectID := range projectIDs {
			if err := storage.LockRepository(s.requestContext(), projectID); err != nil {
				return err
			}
			records, err := storage.RepositoryRecords(s.requestContext(), projectID, "subscription", 0, 100)
			if err != nil {
				return err
			}
			for _, record := range records {
				var value repositoryStoredSubscription
				if err := json.Unmarshal(record.Payload, &value); err != nil {
					return err
				}
				if value.Kind != "webhook" {
					continue
				}
				secret, err := s.repositorySubscriptionSecret(projectID, value)
				if err != nil {
					return fmt.Errorf("decrypt repository subscription %s: %w", value.ID, err)
				}
				if value.CipherKID == s.cipherKeyring.ActiveKID() {
					continue
				}
				value.SecretCiphertext, value.CipherKID, err = encryption.EncryptMCPToken(repositorySubscriptionSecretBinding(projectID, value.ID, secret), s.cipherKeyring)
				if err != nil {
					return err
				}
				value.Generation = record.Generation + 1
				if err := putRepositoryAutomation(s.requestContext(), storage, projectID, "subscription", value.ID, value, value.Generation, &record.Generation); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if transaction, ok := s.persistence.repo.(transactionalRepository); ok {
		return transaction.WithinTransaction(s.requestContext(), rotate)
	}
	return rotate(s.persistence.repo)
}
