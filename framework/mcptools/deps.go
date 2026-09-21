package mcptools

import (
	"context"

	"github.com/maximhq/bifrost/framework/configstore"
	"github.com/maximhq/bifrost/framework/configstore/tables"
	"github.com/maximhq/bifrost/framework/logstore"
	"gorm.io/gorm"
)

// LogReader is the slice of the deployment's telemetry these tools are allowed
// to read.
//
// It exists for two reasons, and the second is the one that forced it.
//
// The plain reason: this is the whole read surface. All queries, no writes and
// nothing that returns key material. Anything a tool can reach is on this list,
// so reviewing what the server can see means reading one interface rather than
// auditing every executor.
//
// The structural reason: plugins/logging depends on framework, so framework
// cannot depend back on it without a module cycle. Declaring the methods here
// and letting logging.LogManager satisfy them structurally is what lets the
// tools live in framework at all.
type LogReader interface {
	Search(ctx context.Context, filters *logstore.SearchFilters, pagination *logstore.PaginationOptions) (*logstore.SearchResult, error)
	GetLog(ctx context.Context, id string) (*logstore.Log, error)
	// GetLogsByIDs hydrates vector-search candidates through the ordinary
	// scoped log reader. Implementations must preserve the input order and omit
	// rows that are missing or outside the caller's query scope.
	GetLogsByIDs(ctx context.Context, ids []string) ([]logstore.Log, error)
	GetStats(ctx context.Context, filters *logstore.SearchFilters) (*logstore.SearchStats, error)

	GetHistogram(ctx context.Context, filters *logstore.SearchFilters, bucketSizeSeconds int64) (*logstore.HistogramResult, error)
	GetCostHistogram(ctx context.Context, filters *logstore.SearchFilters, bucketSizeSeconds int64) (*logstore.CostHistogramResult, error)
	GetTokenHistogram(ctx context.Context, filters *logstore.SearchFilters, bucketSizeSeconds int64) (*logstore.TokenHistogramResult, error)
	GetLatencyHistogram(ctx context.Context, filters *logstore.SearchFilters, bucketSizeSeconds int64) (*logstore.LatencyHistogramResult, error)
	GetThroughputHistogram(ctx context.Context, filters *logstore.SearchFilters, bucketSizeSeconds int64) (*logstore.ThroughputHistogramResult, error)

	GetModelRankings(ctx context.Context, filters *logstore.SearchFilters) (*logstore.ModelRankingResult, error)
	GetDimensionRankings(ctx context.Context, filters *logstore.SearchFilters, dimension logstore.RankingDimension) (*logstore.DimensionRankingResult, error)

	GetProviderCostHistogram(ctx context.Context, filters *logstore.SearchFilters, bucketSizeSeconds int64) (*logstore.ProviderCostHistogramResult, error)
	GetProviderLatencyHistogram(ctx context.Context, filters *logstore.SearchFilters, bucketSizeSeconds int64) (*logstore.ProviderLatencyHistogramResult, error)
	GetProviderThroughputHistogram(ctx context.Context, filters *logstore.SearchFilters, bucketSizeSeconds int64) (*logstore.ProviderThroughputHistogramResult, error)
	GetProviderTokenHistogram(ctx context.Context, filters *logstore.SearchFilters, bucketSizeSeconds int64) (*logstore.ProviderTokenHistogramResult, error)

	GetAvailableModels(ctx context.Context, limit int, query string) ([]string, error)
	GetAvailableApps(ctx context.Context, limit int, query string) ([]string, error)
	GetAvailableStopReasons(ctx context.Context, limit int, query string) ([]string, error)
	// GetAvailableVirtualKeys returns id/name pairs. The type is this package's
	// own rather than the log manager's: the two are field-identical but carry
	// different struct tags, and aliasing them would change the JSON an existing
	// endpoint already serves.
	GetAvailableVirtualKeys(ctx context.Context, limit int, query string) ([]KeyPair, error)
	// GetAvailableTeams, GetAvailableCustomers and GetAvailableBusinessUnits
	// list the id/name pairs seen in logged traffic - the same distinct lookups
	// the Logs filter bar uses. describe_filter_space reads these rather than
	// ranking each dimension: a ranking on the enterprise hierarchy path fans
	// every row out through JSON-array columns, which took tens of seconds on a
	// large table, all to learn which names exist.
	GetAvailableTeams(ctx context.Context, limit int, query string) ([]KeyPair, error)
	GetAvailableCustomers(ctx context.Context, limit int, query string) ([]KeyPair, error)
	GetAvailableBusinessUnits(ctx context.Context, limit int, query string) ([]KeyPair, error)

	GetSessionLogs(ctx context.Context, sessionID string, pagination *logstore.PaginationOptions) (*logstore.SessionDetailResult, error)
	GetSessionSummary(ctx context.Context, sessionID string) (*logstore.SessionSummaryResult, error)
	GetDroppedRequests(ctx context.Context) int64

	GetMCPToolLog(ctx context.Context, id string) (*logstore.MCPToolLog, error)
	SearchMCPToolLogs(ctx context.Context, filters *logstore.MCPToolLogSearchFilters, pagination *logstore.PaginationOptions) (*logstore.MCPToolLogSearchResult, error)
	GetMCPToolLogStats(ctx context.Context, filters *logstore.MCPToolLogSearchFilters) (*logstore.MCPToolLogStats, error)
	GetMCPHistogram(ctx context.Context, filters logstore.MCPToolLogSearchFilters, bucketSizeSeconds int64) (*logstore.MCPHistogramResult, error)
	GetMCPCostHistogram(ctx context.Context, filters logstore.MCPToolLogSearchFilters, bucketSizeSeconds int64) (*logstore.MCPCostHistogramResult, error)
	GetMCPTopTools(ctx context.Context, filters logstore.MCPToolLogSearchFilters, limit int) (*logstore.MCPTopToolsResult, error)
}

// KeyPair is an id paired with the name it is known by.
type KeyPair struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GovernanceReader is the slice of the config store these tools may reach.
// configstore.ConfigStore satisfies it structurally. Every method is
// scope-aware: the caller's ctx narrows which rows come back, the same
// row-level enforcement LogReader gets.
//
// Writes persist through this interface; GovernanceReloader is what makes a
// new or edited key live in the in-memory governance store that inference
// actually consults. A write without a reload leaves a row that cannot
// authenticate until the next process start.
type GovernanceReader interface {
	GetVirtualKey(ctx context.Context, id string) (*tables.TableVirtualKey, error)
	GetVirtualKeysPaginated(ctx context.Context, params configstore.VirtualKeyQueryParams) ([]tables.TableVirtualKey, int64, error)
	CreateVirtualKey(ctx context.Context, virtualKey *tables.TableVirtualKey, tx ...*gorm.DB) error
	UpdateVirtualKey(ctx context.Context, virtualKey *tables.TableVirtualKey, tx ...*gorm.DB) error

	GetTeam(ctx context.Context, id string) (*tables.TableTeam, error)
	GetTeamsPaginated(ctx context.Context, params configstore.TeamsQueryParams) ([]tables.TableTeam, int64, error)
	CreateTeam(ctx context.Context, team *tables.TableTeam, tx ...*gorm.DB) error

	GetCustomer(ctx context.Context, id string) (*tables.TableCustomer, error)
	GetCustomersPaginated(ctx context.Context, params configstore.CustomersQueryParams) ([]tables.TableCustomer, int64, error)
	CreateCustomer(ctx context.Context, customer *tables.TableCustomer, tx ...*gorm.DB) error

	GetBudget(ctx context.Context, id string, tx ...*gorm.DB) (*tables.TableBudget, error)
	GetBudgets(ctx context.Context) ([]tables.TableBudget, error)
	CreateBudget(ctx context.Context, budget *tables.TableBudget, tx ...*gorm.DB) error

	GetProviders(ctx context.Context) ([]tables.TableProvider, error)
	GetMCPClientsPaginated(ctx context.Context, params configstore.MCPClientsQueryParams) ([]tables.TableMCPClient, int64, error)
	GetClientConfig(ctx context.Context) (*configstore.ClientConfig, error)
}

// GovernanceReloader refreshes the in-memory governance cache after a write.
// Inference reads that cache, not the config store, so a create or update that
// skips this leaves a row that cannot authenticate. Nil on a deployment with
// no governance plugin; write tools then persist and report that the cache
// could not be reloaded.
type GovernanceReloader interface {
	ReloadVirtualKey(ctx context.Context, id string) (*tables.TableVirtualKey, error)
	ReloadTeam(ctx context.Context, id string) (*tables.TableTeam, error)
	ReloadCustomer(ctx context.Context, id string) (*tables.TableCustomer, error)
}

// Pinger is a store that can answer whether it is reachable. Config, log and
// vector stores all satisfy it; get_health calls each independently so a down
// log store does not hide a healthy config store.
type Pinger interface {
	Ping(ctx context.Context) error
}

// MaxSemanticQueryChars bounds the natural-language query, in characters. The
// query becomes an embedding request, so an unbounded tool argument is
// provider capacity and usage budget spent on one call. The tool schema
// advertises the same figure as maxLength, so the model can stay inside it
// instead of learning the bound from a refusal. Declared here rather than in
// framework/warp so the schema can read it without an import cycle.
const MaxSemanticQueryChars = 2000

// SemanticSearcher is semantic_search_logs' dependency: a meaning search over
// indexed conversations that hydrates its candidates back through a scoped
// LogReader. framework/warp's SemanticSearcher satisfies it; declaring the
// shape here keeps this package independent of who runs the index.
type SemanticSearcher interface {
	Search(ctx context.Context, query string, filters *logstore.SearchFilters, requestedLimit int) (SemanticSearchResult, error)
}

// SemanticSearchRow is one hit: the projected log row and its vector score.
type SemanticSearchRow struct {
	Score float64 `json:"score"`
	LogRow
}

// SemanticSearchResult is what a SemanticSearcher returns, in score order.
type SemanticSearchResult struct {
	Rows      []SemanticSearchRow `json:"rows"`
	Returned  int                 `json:"returned"`
	Threshold float64             `json:"threshold"`
}
