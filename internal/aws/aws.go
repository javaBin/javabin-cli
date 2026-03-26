package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	defaultRegion   = "eu-central-1"
	CURDatabase     = "javabin_cur"
	CURTable        = "javabin_cur"
	AthenaWorkgroup = "javabin-cost-analytics"
)

func LoadConfig(ctx context.Context) (aws.Config, error) {
	return awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(defaultRegion))
}

// CallerIdentity holds STS identity info.
type CallerIdentity struct {
	Account string
	ARN     string
	UserID  string
}

func GetCallerIdentity(ctx context.Context, cfg aws.Config) (*CallerIdentity, error) {
	client := sts.NewFromConfig(cfg)
	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, err
	}
	return &CallerIdentity{
		Account: aws.ToString(out.Account),
		ARN:     aws.ToString(out.Arn),
		UserID:  aws.ToString(out.UserId),
	}, nil
}

// GetTeamMonthlyCost returns month-to-date cost for a team tag.
func GetTeamMonthlyCost(ctx context.Context, cfg aws.Config, team string) (float64, error) {
	client := costexplorer.NewFromConfig(cfg, func(o *costexplorer.Options) {
		o.Region = "us-east-1" // Cost Explorer is global
	})

	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	out, err := client.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(start.Format("2006-01-02")),
			End:   aws.String(now.Format("2006-01-02")),
		},
		Granularity: cetypes.GranularityMonthly,
		Metrics:     []string{"UnblendedCost"},
		Filter: &cetypes.Expression{
			Tags: &cetypes.TagValues{
				Key:    aws.String("team"),
				Values: []string{team},
			},
		},
	})
	if err != nil {
		return 0, err
	}

	total := 0.0
	for _, result := range out.ResultsByTime {
		if amount, ok := result.Total["UnblendedCost"]; ok {
			var val float64
			_, _ = fmt.Sscanf(aws.ToString(amount.Amount), "%f", &val)
			total += val
		}
	}
	return total, nil
}

// ServiceInfo holds ECS service summary.
type ServiceInfo struct {
	Name         string
	RunningCount int32
	DesiredCount int32
}

// ListServices returns ECS services in a cluster.
func ListServices(ctx context.Context, cfg aws.Config, cluster string) ([]ServiceInfo, error) {
	client := ecs.NewFromConfig(cfg)

	listOut, err := client.ListServices(ctx, &ecs.ListServicesInput{
		Cluster: aws.String(cluster),
	})
	if err != nil {
		return nil, err
	}
	if len(listOut.ServiceArns) == 0 {
		return nil, nil
	}

	descOut, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(cluster),
		Services: listOut.ServiceArns,
	})
	if err != nil {
		return nil, err
	}

	var services []ServiceInfo
	for _, svc := range descOut.Services {
		services = append(services, ServiceInfo{
			Name:         aws.ToString(svc.ServiceName),
			RunningCount: svc.RunningCount,
			DesiredCount: svc.DesiredCount,
		})
	}
	return services, nil
}

// ResourceCost holds a CUR resource-level cost entry.
type ResourceCost struct {
	ResourceID string
	Service    string
	Team       string
	Cost       float64
	UsageType  string
}

// FriendlyName returns a shortened version of the resource ARN.
func (r ResourceCost) FriendlyName() string {
	id := r.ResourceID
	if id == "" {
		return "(no resource ID)"
	}
	if strings.Contains(id, ":::") {
		parts := strings.SplitN(id, ":::", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	if strings.Contains(id, "/") {
		parts := strings.Split(id, "/")
		if len(parts) <= 3 {
			return parts[len(parts)-1]
		}
		return strings.Join(parts[len(parts)-2:], "/")
	}
	if strings.Contains(id, ":") {
		parts := strings.Split(id, ":")
		return parts[len(parts)-1]
	}
	return id
}

// GetTeamResourceCosts queries CUR via Athena for top resources by cost for a team.
func GetTeamResourceCosts(ctx context.Context, cfg aws.Config, team, period string) ([]ResourceCost, error) {
	now := time.Now().UTC()
	year := now.Format("2006")
	month := now.Format("01")

	var dateFilter string
	switch period {
	case "day":
		yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
		today := now.Format("2006-01-02")
		dateFilter = fmt.Sprintf(
			"AND line_item_usage_start_date >= TIMESTAMP '%s' AND line_item_usage_start_date < TIMESTAMP '%s'",
			yesterday, today,
		)
	case "week":
		weekAgo := now.AddDate(0, 0, -7).Format("2006-01-02")
		today := now.Format("2006-01-02")
		dateFilter = fmt.Sprintf(
			"AND line_item_usage_start_date >= TIMESTAMP '%s' AND line_item_usage_start_date < TIMESTAMP '%s'",
			weekAgo, today,
		)
	default: // month
		dateFilter = "" // year/month partition filter is sufficient
	}

	query := fmt.Sprintf(`
		SELECT line_item_resource_id,
		       line_item_product_code,
		       line_item_usage_type,
		       COALESCE(resource_tags_user_team, '') as team,
		       SUM(CAST(line_item_unblended_cost AS double)) as total_cost
		FROM "%s"."%s"
		WHERE year = '%s' AND month = '%s'
		  AND resource_tags_user_team = '%s'
		  AND line_item_resource_id != ''
		  AND line_item_line_item_type = 'Usage'
		  %s
		GROUP BY line_item_resource_id, line_item_product_code,
		         line_item_usage_type, COALESCE(resource_tags_user_team, '')
		HAVING SUM(CAST(line_item_unblended_cost AS double)) >= 0.01
		ORDER BY total_cost DESC
		LIMIT 10
	`, CURDatabase, CURTable, year, month, team, dateFilter)

	rows, err := RunAthenaQuery(ctx, cfg, query, CURDatabase, AthenaWorkgroup)
	if err != nil {
		return nil, err
	}

	var results []ResourceCost
	for _, row := range rows {
		var cost float64
		fmt.Sscanf(row["total_cost"], "%f", &cost)
		results = append(results, ResourceCost{
			ResourceID: row["line_item_resource_id"],
			Service:    row["line_item_product_code"],
			Team:       row["team"],
			Cost:       cost,
			UsageType:  row["line_item_usage_type"],
		})
	}
	return results, nil
}

// RunAthenaQuery executes a query and returns results as a slice of maps.
func RunAthenaQuery(ctx context.Context, cfg aws.Config, query, database, workgroup string) ([]map[string]string, error) {
	client := athena.NewFromConfig(cfg)

	startOut, err := client.StartQueryExecution(ctx, &athena.StartQueryExecutionInput{
		QueryString: aws.String(query),
		QueryExecutionContext: &athenatypes.QueryExecutionContext{
			Database: aws.String(database),
		},
		WorkGroup: aws.String(workgroup),
	})
	if err != nil {
		return nil, fmt.Errorf("start query: %w", err)
	}

	execID := startOut.QueryExecutionId

	// Poll for completion (30s timeout)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		statusOut, err := client.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{
			QueryExecutionId: execID,
		})
		if err != nil {
			return nil, fmt.Errorf("get query status: %w", err)
		}

		state := statusOut.QueryExecution.Status.State
		switch state {
		case athenatypes.QueryExecutionStateSucceeded:
			goto fetchResults
		case athenatypes.QueryExecutionStateFailed, athenatypes.QueryExecutionStateCancelled:
			reason := aws.ToString(statusOut.QueryExecution.Status.StateChangeReason)
			return nil, fmt.Errorf("query %s: %s", state, reason)
		}

		time.Sleep(1 * time.Second)
	}
	return nil, fmt.Errorf("query timed out")

fetchResults:
	resultsOut, err := client.GetQueryResults(ctx, &athena.GetQueryResultsInput{
		QueryExecutionId: execID,
	})
	if err != nil {
		return nil, fmt.Errorf("get results: %w", err)
	}

	resultSet := resultsOut.ResultSet
	if len(resultSet.Rows) < 2 {
		return nil, nil // header only, no data
	}

	// First row is the header
	var columns []string
	for _, col := range resultSet.Rows[0].Data {
		columns = append(columns, aws.ToString(col.VarCharValue))
	}

	var rows []map[string]string
	for _, row := range resultSet.Rows[1:] {
		m := make(map[string]string)
		for i, d := range row.Data {
			if i < len(columns) {
				m[columns[i]] = aws.ToString(d.VarCharValue)
			}
		}
		rows = append(rows, m)
	}
	return rows, nil
}
