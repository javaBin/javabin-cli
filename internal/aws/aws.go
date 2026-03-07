package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const defaultRegion = "eu-central-1"

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

// GetMonthlyCost returns month-to-date cost for a project tag.
func GetMonthlyCost(ctx context.Context, cfg aws.Config, project string) (float64, error) {
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
				Key:    aws.String("service"),
				Values: []string{project},
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
