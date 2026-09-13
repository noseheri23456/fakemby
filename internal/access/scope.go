package access

import (
	"sort"

	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Scope returns a reusable, request-local handle. Its predicate is built only
// for media_items and libraries, never for image/source/user enrichment queries.
// Counts and pagination therefore see precisely the same authorized rows.
func Scope(db *gorm.DB, user *database.User) *gorm.DB {
	p, err := Normalize(user)
	return db.Where(policyScope{policy: p, valid: err == nil}).Session(&gorm.Session{})
}

// WhereItems adds a media-only predicate without contaminating service lookups
// on other tables. The query must be a trusted SQL fragment with bound values.
func WhereItems(db *gorm.DB, query string, args ...any) *gorm.DB {
	return db.Where(itemPredicate{expr: clause.Expr{SQL: query, Vars: args}}).Session(&gorm.Session{})
}

func CanAccessLibrary(db *gorm.DB, user *database.User, libraryID string) bool {
	if db == nil || libraryID == "" {
		return false
	}
	var count int64
	err := Scope(db, user).Model(&database.Library{}).Where("id = ?", libraryID).Count(&count).Error
	return err == nil && count == 1
}

// CanAccessItem also accepts library IDs, which Emby uses as CollectionFolders.
func CanAccessItem(db *gorm.DB, user *database.User, itemID string) bool {
	if db == nil || itemID == "" {
		return false
	}
	var count int64
	err := Scope(db, user).Model(&database.MediaItem{}).Where("id = ?", itemID).Count(&count).Error
	if err != nil {
		return false
	}
	return count == 1 || CanAccessLibrary(db, user, itemID)
}

type policyScope struct {
	policy Policy
	valid  bool
}

func queryTable(builder clause.Builder) string {
	if stmt, ok := builder.(*gorm.Statement); ok {
		if stmt.Schema != nil {
			return stmt.Schema.Table
		}
		return stmt.Table
	}
	return ""
}

func (s policyScope) Build(builder clause.Builder) {
	table := queryTable(builder)
	if table != "media_items" && table != "libraries" {
		_, _ = builder.WriteString("1=1")
		return
	}
	if !s.valid || s.policy.IsDisabled {
		_, _ = builder.WriteString("1=0")
		return
	}
	if table == "libraries" {
		s.policy.libraryPredicate(clause.Column{Table: clause.CurrentTable, Name: "id"}).Build(builder)
		return
	}
	// UNION (not UNION ALL) terminates even if malformed parent links form a
	// cycle. Denial propagates to every descendant, including unrated episodes
	// below a restricted series and items with inconsistent library metadata.
	builder.WriteQuoted(clause.Column{Table: clause.CurrentTable, Name: "id"})
	_, _ = builder.WriteString(" NOT IN (WITH RECURSIVE access_denied(id) AS (SELECT access_item.id FROM media_items AS access_item WHERE NOT COALESCE((")
	s.policy.itemPredicate().Build(builder)
	_, _ = builder.WriteString("), FALSE) UNION SELECT access_child.id FROM media_items AS access_child JOIN access_denied ON access_child.parent_id = access_denied.id) SELECT id FROM access_denied)")
}

func (p Policy) libraryPredicate(column clause.Column) clause.Expression {
	exprs := []clause.Expression{clause.Expr{SQL: "1=1"}}
	if !p.EnableAllFolders {
		exprs = append(exprs, clause.IN{Column: column, Values: stringValues(p.EnabledFolders)})
	}
	if len(p.BlockedMediaFolders) > 0 {
		exprs = append(exprs, clause.Not(clause.IN{Column: column, Values: stringValues(p.BlockedMediaFolders)}))
	}
	return clause.And(exprs...)
}

func (p Policy) itemPredicate() clause.Expression {
	exprs := []clause.Expression{
		clause.Expr{SQL: "COALESCE(access_item.is_hidden, ?) = ?", Vars: []any{false, false}},
		clause.Expr{SQL: "access_item.library_id IN (SELECT id FROM libraries)"},
		p.libraryPredicate(clause.Column{Table: "access_item", Name: "library_id"}),
	}
	const rating = "UPPER(TRIM(COALESCE(access_item.official_rating, '')))"
	var known, permitted []string
	for name, level := range ratingLevels {
		known = append(known, name)
		if p.MaxParentalRating == nil || level <= *p.MaxParentalRating {
			permitted = append(permitted, name)
		}
	}
	sort.Strings(known)
	sort.Strings(permitted)
	if p.MaxParentalRating != nil {
		exprs = append(exprs, clause.Expr{
			SQL:  "(" + rating + " IN ? OR " + rating + " IN ?)",
			Vars: []any{permitted, []string{"", "NR", "UNRATED", "NOT RATED"}},
		})
	}
	if kinds := p.blockedTypes(); len(kinds) > 0 {
		exprs = append(exprs, clause.Expr{
			SQL:  "(" + rating + " IN ? OR LOWER(access_item.type) NOT IN ?)",
			Vars: []any{known, kinds},
		})
	}
	return clause.And(exprs...)
}

func stringValues(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

type itemPredicate struct{ expr clause.Expression }

func (p itemPredicate) Build(builder clause.Builder) {
	if queryTable(builder) == "media_items" {
		p.expr.Build(builder)
	} else {
		_, _ = builder.WriteString("1=1")
	}
}
