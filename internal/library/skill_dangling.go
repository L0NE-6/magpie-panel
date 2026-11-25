package library

// A skill linked in from a folder the user keeps, whose source is gone
// (@Eternalfate__ on X, #7226): "Bring in all" links a skill of the skills
// CLI's ~/.agents/skills into the library instead of moving it, and
// `npx skills remove <name>` then deletes that folder. The library's link
// leads nowhere, and every agent ticked for it was given a link to that
// link. markDangling marks such a skill Gone (the page offers to take it
// out, RemoveGoneSkills, which only removes the link) and Dangling, which
// `gives` reads, so no sync writes an agent a link to it. Nothing the user
// owns is touched: only magpie's own links are taken away, and a folder
// that can't be read is unknown, not gone.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// sourceGone is whether the library's skill by that name is a link to a
// folder that isn't there any more, or has no SKILL.md left. A skill that
// is a folder of the library's own, or a link that can't be looked at, is
// not: unknown keeps the entry.
func sourceGone(name string) bool {
	p := skillDir(name)
	if !linked(p) {
		return false
	}
	fi, err := os.Stat(p)
	if err != nil {
		return absent(err)
	}
	if !fi.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(p, "SKILL.md"))
	return err != nil && absent(err)
}

// markDangling sets Dangling for each skill linked in whose source is
// gone, marks it Gone, and clears that when it is back. It says whether a
// skill's Gone changed (to be saved). A skill from a repository keeps its
// Gone for skill_gone.go's own check.
func (l *Library) markDangling() bool {
	changed := false
	now := time.Now().UTC().Format(time.RFC3339)
	if l.dangling == nil {
		l.dangling = map[string]bool{}
	}
	for _, s := range l.Skills {
		s.Dangling = false
		if !s.Source.remote() {
			switch {
			case sourceGone(s.Name):
				s.Dangling = true
				if s.Gone == "" {
					s.Gone, changed = now, true
				}
			case s.Gone != "" && linked(skillDir(s.Name)):
				if _, err := os.Stat(filepath.Join(skillDir(s.Name), "SKILL.md")); err == nil {
					s.Gone, changed = "", true
				}
			}
		}
		if s.Dangling {
			l.dangling[s.Name] = true
		} else {
			delete(l.dangling, s.Name)
		}
	}
	return changed
}

// healMissingSkillEntries rebuilds the library entry of a folder skill
// from its source folder. A sync must not write agent or router links to a
// library entry that is not there: a registry skill whose entry was lost
// used to make every generated link dangle while the sync still reported
// success (#6697). An entry that cannot be rebuilt is marked dangling, so
// no new link is written, and is reported instead of hidden.
func (l *Library) healMissingSkillEntries(res *Result) {
	if l.dangling == nil {
		l.dangling = map[string]bool{}
	}
	for _, s := range l.Skills {
		at := filepath.Join(skillDir(s.Name), "SKILL.md")
		if _, err := os.Stat(at); err == nil {
			continue
		} else if !absent(err) {
			s.Dangling = true
			l.dangling[s.Name] = true
			res.Problems = append(res.Problems, Problem{
				What:  "skill:" + s.Name,
				Error: fmt.Sprintf("library entry %s cannot be read: %v", skillDir(s.Name), err),
			})
			continue
		}
		if err := restoreSkillEntry(s); err != nil {
			s.Dangling = true
			l.dangling[s.Name] = true
			res.Problems = append(res.Problems, Problem{What: "skill:" + s.Name, Error: err.Error()})
			continue
		}
		s.Dangling = false
		delete(l.dangling, s.Name)
	}
}

// restoreSkillEntry puts back library/skills/<name> for a folder skill
// whose source folder still holds the skill. Only a missing entry or a
// link is replaced; a real folder is never deleted.
func restoreSkillEntry(s *Skill) error {
	lib := skillDir(s.Name)
	if s.Source == nil || s.Source.Kind != "folder" || s.Source.Dir == "" {
		return fmt.Errorf("library entry %s is missing and has no folder source to restore it from", lib)
	}
	src := s.Source.Dir
	fi, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("library entry %s is missing and its source folder %s cannot be read: %w", lib, src, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("library entry %s is missing and its source %s is not a folder", lib, src)
	}
	if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
		return fmt.Errorf("library entry %s is missing and its source folder %s has no readable SKILL.md: %w", lib, src, err)
	}

	fi, err = os.Lstat(lib)
	switch {
	case err == nil && linkEntry(fi):
		if err := os.Remove(lib); err != nil {
			return fmt.Errorf("library entry %s is a broken link that cannot be replaced: %w", lib, err)
		}
	case err == nil:
		return fmt.Errorf("library entry %s exists but has no readable SKILL.md; left it untouched", lib)
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("library entry %s cannot be inspected: %w", lib, err)
	}

	if err := os.MkdirAll(skillsDir(), 0o755); err != nil {
		return fmt.Errorf("library entry %s cannot be restored: %w", lib, err)
	}
	if err := dirLink(src, lib); err != nil {
		return fmt.Errorf("library entry %s cannot link to its source folder %s: %w", lib, src, err)
	}
	if _, err := os.Stat(filepath.Join(lib, "SKILL.md")); err != nil {
		_ = unlink(lib)
		return fmt.Errorf("library entry %s was linked to %s but the result is not readable: %w", lib, src, err)
	}
	return nil
}
