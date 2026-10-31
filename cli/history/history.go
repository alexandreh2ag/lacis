// Package history holds the "history" command of lacis-server.
package history

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/spf13/cobra"
)

const (
	FlagNamespace = "namespace"
	FlagHost      = "host"
	FlagType      = "type"
	FlagSince     = "since"
	FlagLimit     = "limit"
	FlagOldest    = "oldest-first"
	FlagFormat    = "format"

	FormatText = "text"
	FormatJSON = "json"

	DefaultLimit = 50
)

// NewService builds the service used by "history", and the function
// releasing it; tests replace it.
var NewService = func(ctx *context.Context) (service.HistoryService, func(), error) {
	db, err := database.Open(ctx.Config.Database, ctx.Logger)
	if err != nil {
		return nil, nil, err
	}
	return service.NewHistoryService(ctx, repository.NewTransactor(db)), func() { _ = database.Close(db) }, nil
}

var entityTypes = []string{model.ChangeEntityHost, model.ChangeEntityRuntime, model.ChangeEntityService,
	model.ChangeEntityWebserver, model.ChangeEntityApp, model.ChangeEntityLink}

func GetHistoryCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "List the changes of a namespace",
		Long: "List the changes of a namespace, the latest first unless --oldest-first: the hosts, runtimes, services, webservers and " +
			"apps added, removed or changed by their snapshots, and the links added or removed by their computation.",
		Example: "  lacis-server history --namespace acme\n" +
			"  lacis-server history --namespace acme --host acme-shop-prod-1 --since 7d\n" +
			"  lacis-server history --namespace acme --type link --format json\n" +
			"  lacis-server history --namespace acme --since 1d --oldest-first --limit 0",
		Args: cobra.NoArgs,
		RunE: GetHistoryRunFn(ctx),
	}
	cmd.Flags().StringP(FlagNamespace, "n", "", "slug of the namespace")
	cmd.Flags().String(FlagHost, "", "keep the changes of this host (hostname)")
	cmd.Flags().String(FlagType, "", "keep the changes of this kind of entity: "+strings.Join(entityTypes, ", "))
	cmd.Flags().String(FlagSince, "", "keep the changes of this last duration, e.g. 12h, 7d")
	cmd.Flags().Int(FlagLimit, DefaultLimit, "number of changes listed, 0 lists all")
	cmd.Flags().Bool(FlagOldest, false, "list the changes in the order they occurred")
	cmd.Flags().StringP(FlagFormat, "f", FormatText, "output format: text, json")
	_ = cmd.MarkFlagRequired(FlagNamespace)
	return cmd
}

func GetHistoryRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		namespace, _ := cmd.Flags().GetString(FlagNamespace)
		host, _ := cmd.Flags().GetString(FlagHost)
		entityType, _ := cmd.Flags().GetString(FlagType)
		since, _ := cmd.Flags().GetString(FlagSince)
		limit, _ := cmd.Flags().GetInt(FlagLimit)
		oldest, _ := cmd.Flags().GetBool(FlagOldest)
		format, _ := cmd.Flags().GetString(FlagFormat)
		if entityType != "" && !slices.Contains(entityTypes, entityType) {
			return fmt.Errorf("unknown type %q, expected one of: %s", entityType, strings.Join(entityTypes, ", "))
		}
		if format != FormatText && format != FormatJSON {
			return fmt.Errorf("unknown format %q, expected one of: %s, %s", format, FormatText, FormatJSON)
		}
		filter := service.HistoryFilter{Hostname: host, EntityType: entityType, OldestFirst: oldest, Limit: limit}
		if since != "" {
			d, err := config.ParseDuration(since)
			if err != nil {
				return fmt.Errorf("invalid --since: %w", err)
			}
			filter.Since = ctx.Clock.Now().Add(-d)
		}

		svc, release, err := NewService(ctx)
		if err != nil {
			return err
		}
		defer release()
		rows, err := svc.List(cmd.Context(), namespace, filter)
		if err != nil {
			return err
		}
		if format == FormatJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(jsonRows(rows))
		}
		return text(cmd.OutOrStdout(), rows)
	}
}

// event is a change event as the JSON output gives it.
type event struct {
	ID         int64           `json:"id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Host       string          `json:"host,omitempty"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id,omitempty"`
	Kind       string          `json:"kind"`
	Before     json.RawMessage `json:"before,omitempty"`
	After      json.RawMessage `json:"after,omitempty"`
}

func jsonRows(rows []repository.ChangeEventRow) []event {
	out := make([]event, 0, len(rows))
	for _, r := range rows {
		e := event{ID: r.ID, OccurredAt: r.OccurredAt, EntityType: r.EntityType, Kind: r.Kind, Before: r.Before, After: r.After}
		if r.Hostname != nil {
			e.Host = *r.Hostname
		}
		if r.EntityID != nil {
			e.EntityID = r.EntityID.String()
		}
		out = append(out, e)
	}
	return out
}

// labels are the fields naming an entity of each type, in the text output.
var labels = map[string][]string{
	model.ChangeEntityHost:      {"hostname"},
	model.ChangeEntityRuntime:   {"type", "path"},
	model.ChangeEntityService:   {"type", "instance"},
	model.ChangeEntityWebserver: {"type"},
	model.ChangeEntityApp:       {"name", "path"},
}

// text writes one line per event: when, where, what, and the fields that
// changed.
func text(w io.Writer, rows []repository.ChangeEventRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		host := "-"
		if r.Hostname != nil {
			host = *r.Hostname
		}
		before, after := decode(r.Before), decode(r.After)
		fields := after
		if r.Kind == model.ChangeRemoved {
			fields = before
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.OccurredAt.Local().Format(time.DateTime), host, r.EntityType,
			r.Kind, label(r.EntityType, fields), summary(r.Kind, before, after))
	}
	return tw.Flush()
}

func decode(raw json.RawMessage) map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	return m
}

func label(entityType string, fields map[string]any) string {
	if entityType == model.ChangeEntityLink {
		out := value(fields["from"]) + " → " + value(fields["to"])
		if key := value(fields["key"]); key != "" {
			out += " (" + key + ")"
		}
		return out
	}
	var parts []string
	for _, k := range labels[entityType] {
		if v := value(fields[k]); v != "" && v != "default" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}

// summary tells the fields that changed, a field holding an object by its
// own fields; or the version of an entity added or removed, or how a link
// was found.
func summary(kind string, before, after map[string]any) string {
	switch kind {
	case model.ChangeChanged:
		return strings.Join(changes("", before, after), ", ")
	case model.ChangeRemoved:
		after = before
	}
	var parts []string
	for _, k := range []string{"version", "confidence", "rule", "reason"} {
		if v := value(after[k]); v != "" {
			parts = append(parts, k+": "+v)
		}
	}
	return strings.Join(parts, ", ")
}

func changes(prefix string, before, after map[string]any) []string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(union(before, after))) {
		b, bok := before[k].(map[string]any)
		a, aok := after[k].(map[string]any)
		if bok && aok {
			out = append(out, changes(prefix+k+".", b, a)...)
			continue
		}
		if vb, va := value(before[k]), value(after[k]); vb != va {
			out = append(out, fmt.Sprintf("%s%s: %s → %s", prefix, k, or(vb, "∅"), or(va, "∅")))
		}
	}
	return out
}

func union(a, b map[string]any) map[string]bool {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return keys
}

// value prints a field: a string as is, another value as JSON.
func value(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
