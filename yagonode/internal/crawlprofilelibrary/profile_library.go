package crawlprofilelibrary

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const profileBucket vault.Name = "crawl_profile_library"

const (
	MaximumProfiles         = 128
	MaximumProfileNameBytes = 80
	profileIdentityBytes    = 16
)

type SavedProfile struct {
	ID        string                         `json:"id"`
	Profile   yagocrawlcontract.CrawlProfile `json:"profile"`
	CreatedAt time.Time                      `json:"createdAt"`
	UpdatedAt time.Time                      `json:"updatedAt"`
}

type savedProfileCodec struct{}

func (savedProfileCodec) Encode(profile SavedProfile) ([]byte, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("encode saved crawl profile: %w", err)
	}

	return raw, nil
}

func (savedProfileCodec) Decode(raw []byte) (SavedProfile, error) {
	var profile SavedProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return SavedProfile{}, fmt.Errorf("decode saved crawl profile: %w", err)
	}
	if err := validateSavedProfile(profile); err != nil {
		return SavedProfile{}, err
	}

	return profile, nil
}

type Library struct {
	vault   *vault.Vault
	values  *vault.Collection[SavedProfile]
	entropy io.Reader
	now     func() time.Time
}

func Open(storage *vault.Vault) (*Library, error) {
	values, err := vault.Register(storage, profileBucket, savedProfileCodec{})
	if err != nil {
		return nil, fmt.Errorf("register saved crawl profiles: %w", err)
	}

	return &Library{vault: storage, values: values, entropy: rand.Reader, now: time.Now}, nil
}

func (library *Library) Create(
	ctx context.Context,
	profile yagocrawlcontract.CrawlProfile,
) (SavedProfile, error) {
	profile, err := validatedProfile(profile)
	if err != nil {
		return SavedProfile{}, err
	}
	identity, err := library.newIdentity()
	if err != nil {
		return SavedProfile{}, err
	}
	now := library.now().UTC()
	saved := SavedProfile{ID: identity, Profile: profile, CreatedAt: now, UpdatedAt: now}
	if err := library.vault.Update(ctx, func(transaction *vault.Txn) error {
		length, err := library.values.Len(transaction)
		if err != nil {
			return fmt.Errorf("count saved crawl profiles: %w", err)
		}
		if length >= MaximumProfiles {
			return fmt.Errorf("saved crawl profile limit is %d", MaximumProfiles)
		}
		if library.values.Contains(transaction, vault.Key(identity)) {
			return fmt.Errorf("saved crawl profile identity collision")
		}
		if err := library.rejectDuplicateName(transaction, "", profile.Name); err != nil {
			return err
		}

		return library.values.Put(transaction, vault.Key(identity), saved)
	}); err != nil {
		return SavedProfile{}, fmt.Errorf("create saved crawl profile: %w", err)
	}

	return saved, nil
}

func (library *Library) Update(
	ctx context.Context,
	identity string,
	profile yagocrawlcontract.CrawlProfile,
) (SavedProfile, error) {
	if !validProfileIdentity(identity) {
		return SavedProfile{}, fmt.Errorf("invalid saved crawl profile identity")
	}
	profile, err := validatedProfile(profile)
	if err != nil {
		return SavedProfile{}, err
	}
	var updated SavedProfile
	if err := library.vault.Update(ctx, func(transaction *vault.Txn) error {
		stored, found, err := library.values.Get(transaction, vault.Key(identity))
		if err != nil {
			return fmt.Errorf("read saved crawl profile: %w", err)
		}
		if !found {
			return fmt.Errorf("no saved crawl profile %q", identity)
		}
		if err := library.rejectDuplicateName(transaction, identity, profile.Name); err != nil {
			return err
		}
		stored.Profile = profile
		stored.UpdatedAt = library.now().UTC()
		updated = stored

		return library.values.Put(transaction, vault.Key(identity), stored)
	}); err != nil {
		return SavedProfile{}, fmt.Errorf("update saved crawl profile: %w", err)
	}

	return updated, nil
}

func (library *Library) Delete(ctx context.Context, identity string) error {
	if !validProfileIdentity(identity) {
		return fmt.Errorf("invalid saved crawl profile identity")
	}
	if err := library.vault.Update(ctx, func(transaction *vault.Txn) error {
		deleted, err := library.values.Delete(transaction, vault.Key(identity))
		if err != nil {
			return fmt.Errorf("delete saved crawl profile: %w", err)
		}
		if !deleted {
			return fmt.Errorf("no saved crawl profile %q", identity)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update saved crawl profiles: %w", err)
	}

	return nil
}

func (library *Library) Profile(ctx context.Context, identity string) (SavedProfile, error) {
	if !validProfileIdentity(identity) {
		return SavedProfile{}, fmt.Errorf("invalid saved crawl profile identity")
	}
	var profile SavedProfile
	if err := library.vault.View(ctx, func(transaction *vault.Txn) error {
		stored, found, err := library.values.Get(transaction, vault.Key(identity))
		if err != nil {
			return fmt.Errorf("read saved crawl profile: %w", err)
		}
		if !found {
			return fmt.Errorf("no saved crawl profile %q", identity)
		}
		profile = stored

		return nil
	}); err != nil {
		return SavedProfile{}, fmt.Errorf("load saved crawl profile: %w", err)
	}

	return profile, nil
}

func (library *Library) Profiles(ctx context.Context) ([]SavedProfile, error) {
	profiles := make([]SavedProfile, 0)
	if err := library.vault.View(ctx, func(transaction *vault.Txn) error {
		return library.values.Scan(transaction, nil, func(
			_ vault.Key,
			profile SavedProfile,
		) (bool, error) {
			profiles = append(profiles, profile)

			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("list saved crawl profiles: %w", err)
	}
	sort.Slice(profiles, func(left, right int) bool {
		return canonicalProfileName(profiles[left].Profile.Name) <
			canonicalProfileName(profiles[right].Profile.Name)
	})

	return profiles, nil
}

func (library *Library) rejectDuplicateName(
	transaction *vault.Txn,
	exceptIdentity string,
	name string,
) error {
	want := canonicalProfileName(name)
	if err := library.values.Scan(transaction, nil, func(
		key vault.Key,
		profile SavedProfile,
	) (bool, error) {
		if string(key) != exceptIdentity && canonicalProfileName(profile.Profile.Name) == want {
			return false, fmt.Errorf("saved crawl profile name %q already exists", name)
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("scan saved crawl profile names: %w", err)
	}

	return nil
}

func (library *Library) newIdentity() (string, error) {
	raw := make([]byte, profileIdentityBytes)
	if _, err := io.ReadFull(library.entropy, raw); err != nil {
		return "", fmt.Errorf("create saved crawl profile identity: %w", err)
	}

	return hex.EncodeToString(raw), nil
}

func validatedProfile(
	profile yagocrawlcontract.CrawlProfile,
) (yagocrawlcontract.CrawlProfile, error) {
	profile.Name = strings.Join(strings.Fields(profile.Name), " ")
	if profile.Name == "" || len(profile.Name) > MaximumProfileNameBytes ||
		!utf8.ValidString(profile.Name) {
		return yagocrawlcontract.CrawlProfile{}, fmt.Errorf(
			"saved crawl profile name must be 1 to %d UTF-8 bytes",
			MaximumProfileNameBytes,
		)
	}
	profile = yagocrawlcontract.NewCrawlProfile(profile)
	if err := profile.Validate(); err != nil {
		return yagocrawlcontract.CrawlProfile{}, fmt.Errorf("invalid saved crawl profile: %w", err)
	}

	return profile, nil
}

func validateSavedProfile(profile SavedProfile) error {
	if !validProfileIdentity(profile.ID) || profile.CreatedAt.IsZero() ||
		profile.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid saved crawl profile")
	}
	name := strings.Join(strings.Fields(profile.Profile.Name), " ")
	computed := yagocrawlcontract.NewCrawlProfile(profile.Profile)
	if name != profile.Profile.Name || computed.Handle != profile.Profile.Handle ||
		profile.Profile.Validate() != nil {
		return fmt.Errorf("invalid saved crawl profile")
	}

	return nil
}

func validProfileIdentity(identity string) bool {
	if len(identity) != profileIdentityBytes*2 {
		return false
	}
	_, err := hex.DecodeString(identity)

	return err == nil
}

func canonicalProfileName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}
