package mcputil

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// K8sOp starts a traced Kubernetes API operation span.
//
// Returns a context with the span and a finish function that must be called
// when the operation completes. The finish function records result count,
// duration, and any error.
//
// This is a thin wrapper that does NOT import client-go — tool servers
// use their own k8s clients and wrap calls with this for tracing.
//
// Usage:
//
//	ctx, finish := mcputil.K8sOp(ctx, "list", "pods", "agents")
//	pods, err := clientset.CoreV1().Pods("agents").List(ctx, metav1.ListOptions{})
//	finish(len(pods.Items), err)
func K8sOp(ctx context.Context, verb, resource, namespace string) (context.Context, func(resultCount int, err error)) {
	spanName := "k8s." + verb + "." + resource

	attrs := []attribute.KeyValue{
		attribute.String("k8s.verb", verb),
		attribute.String("k8s.resource", resource),
	}
	if namespace != "" {
		attrs = append(attrs, attribute.String("k8s.namespace", namespace))
	}

	ctx, span := Tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	start := time.Now()

	return ctx, func(resultCount int, err error) {
		elapsed := time.Since(start)
		span.SetAttributes(
			attribute.Float64("k8s.duration_ms", float64(elapsed.Milliseconds())),
			attribute.Int("k8s.result_count", resultCount),
		)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

// K8sOpSimple is like K8sOp but for operations that don't return a count
// (e.g. get, delete, patch). Returns a finish function that takes only an error.
//
// Usage:
//
//	ctx, finish := mcputil.K8sOpSimple(ctx, "get", "deployment", "agents")
//	deploy, err := clientset.AppsV1().Deployments("agents").Get(ctx, "my-app", metav1.GetOptions{})
//	finish(err)
func K8sOpSimple(ctx context.Context, verb, resource, namespace string) (context.Context, func(err error)) {
	ctx, finish := K8sOp(ctx, verb, resource, namespace)
	return ctx, func(err error) {
		finish(0, err)
	}
}
