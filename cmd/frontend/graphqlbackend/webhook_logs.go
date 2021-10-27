package graphqlbackend

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"

	"github.com/sourcegraph/sourcegraph/cmd/frontend/backend"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/graphqlbackend/graphqlutil"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/encryption/keyring"
	"github.com/sourcegraph/sourcegraph/internal/types"
)

type webhookLogsArgs struct {
	First      *int
	After      *string
	OnlyErrors *bool
	Since      *time.Time
	Until      *time.Time
}

func (args *webhookLogsArgs) toListOpts(externalServiceID int64) (database.WebhookLogsListOpts, error) {
	opts := database.WebhookLogsListOpts{
		ExternalServiceID: &externalServiceID,
		Since:             args.Since,
		Until:             args.Until,
	}

	if args.First != nil {
		opts.Limit = *args.First
	} else {
		opts.Limit = 50
	}

	if args.After != nil {
		var err error
		opts.Cursor, err = strconv.ParseInt(*args.After, 10, 64)
		if err != nil {
			return opts, errors.Wrap(err, "parsing the after cursor")
		}
	}

	if args.OnlyErrors != nil && *args.OnlyErrors {
		opts.OnlyErrors = true
	}

	return opts, nil
}

func (r *schemaResolver) WebhookLogs(ctx context.Context, args *webhookLogsArgs) (*webhookLogConnectionResolver, error) {
	return newWebhookLogConnectionResolver(ctx, r.db, args, 0)
}

type webhookLogConnectionResolver struct {
	args              *webhookLogsArgs
	externalServiceID int64
	store             *database.WebhookLogsStore

	once sync.Once
	logs []*types.WebhookLog
	next int64
	err  error

	totalCountOnce sync.Once
	totalCount     int64
	totalCountErr  error
}

func newWebhookLogConnectionResolver(ctx context.Context, db dbutil.DB, args *webhookLogsArgs, externalServiceID int64) (*webhookLogConnectionResolver, error) {
	if err := backend.CheckCurrentUserIsSiteAdmin(ctx, db); err != nil {
		return nil, err
	}

	return &webhookLogConnectionResolver{
		args:              args,
		externalServiceID: externalServiceID,
		store:             database.WebhookLogs(db, keyring.Default().WebhookLogKey),
	}, nil
}

func (r *webhookLogConnectionResolver) Nodes(ctx context.Context) ([]*webhookLogResolver, error) {
	if err := r.compute(ctx); err != nil {
		return nil, err
	}

	nodes := make([]*webhookLogResolver, len(r.logs))
	for i, log := range r.logs {
		nodes[i] = &webhookLogResolver{
			db:  r.store.Handle().DB(),
			log: log,
		}
	}

	return nodes, nil
}

func (r *webhookLogConnectionResolver) TotalCount(ctx context.Context) (int32, error) {
	r.totalCountOnce.Do(func() {
		r.totalCountErr = func() error {
			opts, err := r.args.toListOpts(r.externalServiceID)
			if err != nil {
				return err
			}

			r.totalCount, err = r.store.Count(ctx, opts)
			return err
		}()
	})

	return int32(r.totalCount), r.totalCountErr
}

func (r *webhookLogConnectionResolver) PageInfo(ctx context.Context) (*graphqlutil.PageInfo, error) {
	if err := r.compute(ctx); err != nil {
		return nil, err
	}

	if r.next == 0 {
		return graphqlutil.HasNextPage(false), nil
	}
	return graphqlutil.NextPageCursor(fmt.Sprint(r.next)), nil
}

func (r *webhookLogConnectionResolver) compute(ctx context.Context) error {
	r.once.Do(func() {
		r.err = func() error {
			opts, err := r.args.toListOpts(r.externalServiceID)
			if err != nil {
				return err
			}

			r.logs, r.next, err = r.store.List(ctx, opts)
			return err
		}()
	})

	return r.err
}

type webhookLogResolver struct {
	db  dbutil.DB
	log *types.WebhookLog
}

func marshalWebhookLogID(id int64) graphql.ID {
	return relay.MarshalID("WebhookLog", id)
}

func unmarshalWebhookLogID(id graphql.ID) (logID int64, err error) {
	err = relay.UnmarshalSpec(id, &logID)
	return
}

func webhookLogByID(ctx context.Context, db dbutil.DB, gqlID graphql.ID) (*webhookLogResolver, error) {
	if err := backend.CheckCurrentUserIsSiteAdmin(ctx, db); err != nil {
		return nil, err
	}

	id, err := unmarshalWebhookLogID(gqlID)
	if err != nil {
		return nil, err
	}

	log, err := database.WebhookLogs(db, keyring.Default().WebhookLogKey).GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return &webhookLogResolver{db: db, log: log}, nil
}

func (r *webhookLogResolver) ID() graphql.ID {
	return marshalWebhookLogID(r.log.ID)
}

func (r *webhookLogResolver) ReceivedAt() DateTime {
	return DateTime{Time: r.log.ReceivedAt}
}

func (r *webhookLogResolver) ExternalService(ctx context.Context) (*externalServiceResolver, error) {
	if r.log.ExternalServiceID == nil {
		return nil, errors.New("no external service attached to webhook log")
	}

	return externalServiceByID(ctx, r.db, marshalExternalServiceID(*r.log.ExternalServiceID))
}

func (r *webhookLogResolver) Request() *webhookLogRequestResolver {
	return &webhookLogRequestResolver{request: &r.log.Request}
}

func (r *webhookLogResolver) Error() *string {
	return r.log.Error
}

type webhookLogRequestResolver struct {
	request *types.WebhookLogRequest
}

func (r *webhookLogRequestResolver) Headers() []*webhookLogRequestHeaderResolver {
	headers := make([]*webhookLogRequestHeaderResolver, 0, len(r.request.Headers))
	for k, v := range r.request.Headers {
		headers = append(headers, &webhookLogRequestHeaderResolver{
			name:   k,
			values: v,
		})
	}

	return headers
}

func (r *webhookLogRequestResolver) Body() string {
	return string(r.request.Body)
}

type webhookLogRequestHeaderResolver struct {
	name   string
	values []string
}

func (r *webhookLogRequestHeaderResolver) Name() string {
	return r.name
}

func (r *webhookLogRequestHeaderResolver) Values() []string {
	return r.values
}
