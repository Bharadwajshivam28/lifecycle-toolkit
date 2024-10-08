package elasticsearch

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "time"

    "github.com/go-logr/logr"
    metricsapi "github.com/keptn/lifecycle-toolkit/metrics-operator/api/v1"
    elastic "github.com/elastic/go-elasticsearch/v8"
    "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
    warningLogStringElastic = "%s API returned warnings: %s"
)

type KeptnElasticProvider struct {
    Log       logr.Logger
    K8sClient client.Client
    Elastic   *elastic.Client
}

func NewElasticProvider(log logr.Logger, k8sClient client.Client, elasticURL string) (*KeptnElasticProvider, error) {
    cfg := elastic.Config{
        Addresses: []string{
            elasticURL,
        },
    }
    es, err := elastic.NewClient(cfg)
    if err != nil {
        return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
    }

    return &KeptnElasticProvider{
        Log:       log,
        K8sClient: k8sClient,
        Elastic:   es,
    }, nil
}

func (r *KeptnElasticProvider) FetchAnalysisValue(ctx context.Context, query string, analysis metricsapi.Analysis, provider *metricsapi.KeptnMetricsProvider) (string, error) {
    ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
    defer cancel()

    result, err := r.runElasticQuery(ctx, query, analysis.GetFrom(), analysis.GetTo())
    if err != nil {
        return "", err
    }

    r.Log.Info(fmt.Sprintf("Elasticsearch query result: %v", result))
    return r.extractMetric(result)
}

func (r *KeptnElasticProvider) EvaluateQuery(ctx context.Context, metric metricsapi.KeptnMetric, provider metricsapi.KeptnMetricsProvider) (string, []byte, error) {
    ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
    defer cancel()

    result, err := r.runElasticQuery(ctx, metric.Spec.Query, time.Now().Add(-30*time.Minute), time.Now())
    if err != nil {
        return "", nil, err
    }

    metricValue, err := r.extractMetric(result)
    if err != nil {
        return "", nil, err
    }

    return metricValue, []byte{}, nil
}

func (r *KeptnElasticProvider) runElasticQuery(ctx context.Context, query string, from, to time.Time) (map[string]interface{}, error) {
    queryBody := fmt.Sprintf(`
    {
        "query": {
            "range": {
                "@timestamp": {
                    "gte": "%s",
                    "lte": "%s"
                }
            }
        }
    }`, from.Format(time.RFC3339), to.Format(time.RFC3339))

    res, err := r.Elastic.Search(
        r.Elastic.Search.WithContext(ctx),
        r.Elastic.Search.WithBody(strings.NewReader(queryBody)),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to execute Elasticsearch query: %w", err)
    }
    defer res.Body.Close()

    if warnings, ok := res.Header["Warning"]; ok {
        r.Log.Info(fmt.Sprintf(warningLogStringElastic, "Elasticsearch", warnings))
    }

    var result map[string]interface{}
    if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("failed to parse Elasticsearch response: %w", err)
    }
    return result, nil
}

func (r *KeptnElasticProvider) extractMetric(result map[string]interface{}) (string, error) {
    hits, ok := result["hits"].(map[string]interface{})
    if !ok {
        return "", fmt.Errorf("invalid result format: missing 'hits' field")
    }

    totalHits, ok := hits["total"].(map[string]interface{})
    if !ok {
        return "", fmt.Errorf("invalid result format: missing 'total' field in 'hits'")
    }

    value := fmt.Sprintf("%v", totalHits["value"])

    return value, nil
}
