package feedback

import (
	"context"
	"errors"
	"net"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

type mongoRepository struct {
	client     *mongo.Client
	collection *mongo.Collection
}

func openMongo(ctx context.Context, c Config) (repository, error) {
	if !databaseName.MatchString(c.Database) {
		return nil, ErrInvalid
	}
	opts := options.Client().ApplyURI(c.DSN).SetConnectTimeout(5 * time.Second).SetServerSelectionTimeout(5 * time.Second).SetTimeout(10 * time.Second).SetMaxPoolSize(8).SetWriteConcern(writeconcern.Majority())
	if e := opts.Validate(); e != nil {
		return nil, ErrInvalid
	}
	if opts.TLSConfig != nil && opts.TLSConfig.InsecureSkipVerify {
		return nil, ErrInvalid
	}
	for _, address := range opts.Hosts {
		host, _, e := net.SplitHostPort(address)
		if e != nil {
			host = address
		}
		if !localHost(host) && opts.TLSConfig == nil {
			return nil, ErrInvalid
		}
	}
	client, e := mongo.Connect(opts)
	if e != nil {
		return nil, e
	}
	closeOnFailure := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(cleanup)
	}
	if e = client.Ping(ctx, readpref.Primary()); e != nil {
		closeOnFailure()
		return nil, e
	}
	collection := client.Database(c.Database).Collection(table)
	_, e = collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "scope", Value: 1}, {Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("feedback_scope_id_v1")},
		{Keys: bson.D{{Key: "scope", Value: 1}, {Key: "created", Value: -1}, {Key: "id", Value: -1}}, Options: options.Index().SetName("feedback_scope_created_v1")},
		{Keys: bson.D{{Key: "expires", Value: 1}}, Options: options.Index().SetName("feedback_expires_v1")},
	})
	if e != nil {
		closeOnFailure()
		return nil, e
	}
	return &mongoRepository{client, collection}, nil
}
func (r *mongoRepository) Insert(ctx context.Context, v row) (bool, error) {
	_, e := r.collection.InsertOne(ctx, v)
	if mongo.IsDuplicateKeyError(e) {
		return false, nil
	}
	return e == nil, e
}
func (r *mongoRepository) Get(ctx context.Context, scope, id string, now int64) (row, error) {
	var v row
	e := r.collection.FindOne(ctx, bson.M{"scope": scope, "id": id, "expires": bson.M{"$gt": now}}).Decode(&v)
	if errors.Is(e, mongo.ErrNoDocuments) {
		e = ErrNotFound
	}
	return v, e
}
func (r *mongoRepository) List(ctx context.Context, scope string, c cursor, limit int, now int64) ([]row, error) {
	f := bson.M{"scope": scope, "expires": bson.M{"$gt": now}}
	if c.ID != "" {
		f["$or"] = bson.A{bson.M{"created": bson.M{"$lt": c.Created}}, bson.M{"created": c.Created, "id": bson.M{"$lt": c.ID}}}
	}
	cur, e := r.collection.Find(ctx, f, options.Find().SetSort(bson.D{{Key: "created", Value: -1}, {Key: "id", Value: -1}}).SetLimit(int64(limit)))
	if e != nil {
		return nil, e
	}
	defer cur.Close(ctx)
	var out []row
	e = cur.All(ctx, &out)
	return out, e
}
func (r *mongoRepository) Replace(ctx context.Context, v row, version int64) (bool, error) {
	result, e := r.collection.UpdateOne(ctx, bson.M{"scope": v.Scope, "id": v.ID, "revision": version, "expires": bson.M{"$gt": time.Now().UnixMilli()}}, bson.M{"$set": bson.M{"payload": v.Payload, "revision": v.Revision}})
	if e != nil {
		return false, e
	}
	return result.MatchedCount == 1, nil
}
func (r *mongoRepository) Delete(ctx context.Context, scope, id string) error {
	_, e := r.collection.DeleteOne(ctx, bson.M{"scope": scope, "id": id})
	return e
}
func (r *mongoRepository) Prune(ctx context.Context, now int64) error {
	_, e := r.collection.DeleteMany(ctx, bson.M{"expires": bson.M{"$lte": now}})
	return e
}
func (r *mongoRepository) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.client.Disconnect(ctx)
}
