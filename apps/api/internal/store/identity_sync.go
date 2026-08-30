package store

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
)

// IdentitySyncProfile accepts the profile fields from OpenClaw's users.list export.
type IdentitySyncProfile struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Emails      []string `json:"emails"`
	MergedInto  *string  `json:"mergedInto"`
}

type IdentitySyncInput struct {
	Source   string
	Profiles []IdentitySyncProfile
}

type IdentitySyncReport struct {
	Linked    int      `json:"linked"`
	Updated   int      `json:"updated"`
	Unchanged int      `json:"unchanged"`
	Unmatched []string `json:"unmatched_profile_ids"`
	Merged    int      `json:"merged_skipped"`
}

// IdentitySyncRow is an existing identity and its user, read in the write transaction.
type IdentitySyncRow struct {
	User
	Provider, Subject, Email string
}

type IdentitySyncChange struct {
	User
	ProfileID string
	Link      bool
	Update    bool
}

func NormalizeIdentitySync(input IdentitySyncInput) (IdentitySyncInput, error) {
	source, err := url.Parse(strings.TrimSpace(input.Source))
	if err != nil || source.Host == "" || source.User != nil || source.Opaque != "" ||
		(source.Scheme != "http" && source.Scheme != "https") ||
		(source.Path != "" && source.Path != "/") || source.RawQuery != "" || source.ForceQuery || source.Fragment != "" {
		return input, errors.New("source must be an absolute HTTP(S) origin without credentials, path, query, or fragment")
	}
	source.Host = strings.ToLower(source.Host)
	if (source.Scheme == "https" && source.Port() == "443") || (source.Scheme == "http" && source.Port() == "80") {
		source.Host = strings.TrimSuffix(source.Host, ":"+source.Port())
	}
	source.Path, source.RawPath = "", ""
	input.Source = source.String()
	if len(input.Source) > 300 || input.Profiles == nil || len(input.Profiles) > 10_000 {
		return input, errors.New("identity sync requires a profiles array of at most 10000 entries and a source of at most 300 bytes")
	}
	ids, emails := map[string]bool{}, map[string]string{}
	for index := range input.Profiles {
		profile := &input.Profiles[index]
		profile.ID = strings.TrimSpace(profile.ID)
		profile.DisplayName = strings.TrimSpace(profile.DisplayName)
		if profile.ID == "" || len(profile.ID) > 128 || strings.ContainsAny(profile.ID, "\r\n\x00") || ids[profile.ID] {
			return input, fmt.Errorf("invalid or duplicate profile ID at index %d", index)
		}
		ids[profile.ID] = true
		if len(profile.DisplayName) > 80 || len(profile.Emails) > 64 || len(identitySyncAvatar(input.Source, profile.ID)) > 500 {
			return input, fmt.Errorf("profile %q exceeds display name (80 bytes), email count (64), or avatar URL (500 bytes) limits", profile.ID)
		}
		for i, email := range profile.Emails {
			email = strings.ToLower(strings.TrimSpace(email))
			parsed, err := mail.ParseAddress(email)
			if err != nil || parsed.Address != email || parsed.Name != "" || len(email) > 320 {
				return input, fmt.Errorf("profile %q has an invalid email alias", profile.ID)
			}
			if profile.MergedInto == nil {
				if owner := emails[email]; owner != "" && owner != profile.ID {
					return input, fmt.Errorf("email alias is shared by profiles %q and %q", owner, profile.ID)
				}
				emails[email] = profile.ID
			}
			profile.Emails[i] = email
		}
	}
	return input, nil
}

func identitySyncAvatar(source, profileID string) string {
	return source + "/api/users/" + url.PathEscape(profileID) + "/avatar"
}

// PlanIdentitySync validates every match before either SQL backend writes. A source
// identity is authoritative after linking; aliases may confirm it, never move it.
func PlanIdentitySync(input IdentitySyncInput, rows []IdentitySyncRow) ([]IdentitySyncChange, IdentitySyncReport, error) {
	report := IdentitySyncReport{Unmatched: []string{}}
	byEmail := map[string]map[string]User{}
	bySubject, subjectByUser := map[string]User{}, map[string]string{}
	fallbacks := map[string]map[string]bool{}
	for _, row := range rows {
		email := strings.ToLower(strings.TrimSpace(row.Email))
		if email != "" {
			if byEmail[email] == nil {
				byEmail[email] = map[string]User{}
			}
			byEmail[email][row.ID] = row.User
			if fallbacks[row.ID] == nil {
				fallbacks[row.ID] = map[string]bool{}
			}
			fallbacks[row.ID][ResolveAvatarURL("", email)] = true
		}
		if row.Provider == input.Source {
			if previous := subjectByUser[row.ID]; previous != "" && previous != row.Subject {
				return nil, report, fmt.Errorf("user %q has multiple identities for this source", row.ID)
			}
			bySubject[row.Subject], subjectByUser[row.ID] = row.User, row.Subject
		}
	}
	changes := make([]IdentitySyncChange, 0, len(input.Profiles))
	claimed := map[string]string{}
	for _, profile := range input.Profiles {
		if profile.MergedInto != nil {
			report.Merged++
			continue
		}
		user, linked := bySubject[profile.ID]
		for _, email := range profile.Emails {
			matches := byEmail[email]
			if len(matches) > 1 {
				return nil, report, fmt.Errorf("profile %q matches an ambiguous email; resolve the existing ClickClack identities first", profile.ID)
			}
			for _, match := range matches {
				if user.ID != "" && user.ID != match.ID {
					return nil, report, fmt.Errorf("profile %q aliases conflict with an existing identity", profile.ID)
				}
				user = match
			}
		}
		if user.ID == "" {
			report.Unmatched = append(report.Unmatched, profile.ID)
			continue
		}
		if user.Kind != "human" {
			return nil, report, fmt.Errorf("profile %q matches a non-human user", profile.ID)
		}
		if subject := subjectByUser[user.ID]; subject != "" && subject != profile.ID {
			return nil, report, fmt.Errorf("user %q is already linked to a different source profile", user.ID)
		}
		if previous := claimed[user.ID]; previous != "" && previous != profile.ID {
			return nil, report, fmt.Errorf("profiles %q and %q match the same user", previous, profile.ID)
		}
		claimed[user.ID] = profile.ID
		change := IdentitySyncChange{User: user, ProfileID: profile.ID, Link: !linked}
		if profile.DisplayName != "" {
			change.DisplayName = profile.DisplayName
		}
		// Only generated fallbacks become source-managed. Explicit ClickClack
		// avatars remain operator-owned; the stable source URL needs no refresh.
		if user.AvatarURL == "" || fallbacks[user.ID][user.AvatarURL] {
			change.AvatarURL = identitySyncAvatar(input.Source, profile.ID)
		}
		change.Update = change.DisplayName != user.DisplayName || change.AvatarURL != user.AvatarURL
		if change.Link {
			report.Linked++
		}
		if change.Update {
			report.Updated++
		}
		if !change.Link && !change.Update {
			report.Unchanged++
		}
		changes = append(changes, change)
	}
	return changes, report, nil
}
