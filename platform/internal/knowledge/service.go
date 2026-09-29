package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (s Service) Execute(ctx context.Context, actor string, command Command) (Result, error) {
	return s.execute(ctx, actor, command, false, nil)
}

// Assess can only classify this actor's pending proposal. No public route or
// conversational tool exposes it. It cannot curate, approve or publish a fact.
func (s Service) Assess(ctx context.Context, actor string, input Assessment) (Result, error) {
	var scope string
	err := s.DB.QueryRow(ctx, `SELECT scope FROM core.knowledge_proposals WHERE id=$1 AND owner=$2`, input.ProposalID, actor).
		Scan(&scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, missing()
	}
	if err != nil {
		return Result{}, err
	}
	decision := "reject"
	if input.Worthwhile {
		decision = approve
	}
	return s.execute(ctx, actor, Command{Name: assess, Key: input.Key, Event: scope, ProposalID: input.ProposalID,
		Version: input.Version, Text: input.Reason, Decision: decision}, true, nil)
}

func (s Service) execute(
	ctx context.Context,
	actor string,
	c Command,
	internal bool,
	sourceKeys []string,
) (Result, error) {
	if err := validate(c, internal); err != nil {
		return Result{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockActor(ctx, tx, actor); err != nil {
		return Result{}, err
	}
	if c.Name != MemoSet && c.Name != MemoDelete && c.Name != DocumentSet && c.Name != DocumentDelete {
		if err = lockScope(ctx, tx, c.Event); err != nil {
			return Result{}, err
		}
	}
	if err = authorize(ctx, tx, actor, c); err != nil {
		return Result{}, err
	}
	sources, err := resolveMemorySources(ctx, tx, actor, sourceKeys)
	if err != nil {
		return Result{}, err
	}
	encoded, err := memoryCommandBytes(c, sourceKeys)
	if err != nil {
		return Result{}, err
	}
	keyHash, requestHash := digest([]byte(c.Key)), digest(encoded)
	previous, found, err := replay(ctx, tx, actor, keyHash, requestHash)
	if err != nil {
		return Result{}, err
	}
	if found {
		return previous, tx.Commit(ctx)
	}
	result, err := mutate(ctx, tx, actor, c)
	if err != nil {
		return Result{}, err
	}
	if err = bindMemorySources(ctx, tx, actor, result, sources); err != nil {
		return Result{}, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return Result{}, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.knowledge_operations(actor,key_hash,request_hash,result) VALUES($1,$2,$3,$4)`,
		actor,
		keyHash,
		requestHash,
		data,
	)
	if err != nil {
		return Result{}, err
	}
	subject, version := resultSubject(result)
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.knowledge_audit(actor,scope,action,subject,version) VALUES($1,$2,$3,$4,$5)`,
		actor,
		c.Event,
		c.Name,
		subject,
		version,
	)
	if err != nil {
		return Result{}, err
	}
	return result, tx.Commit(ctx)
}

func lockActor(ctx context.Context, tx pgx.Tx, actor string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM core.users WHERE id=$1 FOR UPDATE`, actor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return forbidden()
	}
	return err
}

func lockScope(ctx context.Context, tx pgx.Tx, scope string) error {
	if scope != "" {
		var id string
		err := tx.QueryRow(ctx, `SELECT id FROM core.events WHERE id=$1`, scope).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return missing()
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO core.knowledge_scopes(scope,event_id) VALUES($1,$1) ON CONFLICT DO NOTHING`,
			scope,
		); err != nil {
			return err
		}
	}
	var found string
	return tx.QueryRow(ctx, `SELECT scope FROM core.knowledge_scopes WHERE scope=$1 FOR UPDATE`, scope).Scan(&found)
}

func authorize(ctx context.Context, tx pgx.Tx, actor string, c Command) error {
	var permission string
	switch c.Name {
	case Curate, RemoveFact:
		permission = Curate
	case Review:
		permission = Review
	default:
		return nil
	}
	var found string
	err := tx.QueryRow(ctx, `SELECT permission FROM core.knowledge_permissions WHERE scope=$1 AND actor=$2 AND permission=$3 FOR SHARE`, c.Event, actor, permission).
		Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return forbidden()
	}
	return err
}

func replay(ctx context.Context, tx pgx.Tx, actor, key, hash string) (Result, bool, error) {
	var previous string
	var data []byte
	err := tx.QueryRow(ctx, `SELECT request_hash,result FROM core.knowledge_operations WHERE actor=$1 AND key_hash=$2`, actor, key).
		Scan(&previous, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if previous != hash {
		return Result{}, false, conflict("idempotency_conflict")
	}
	var result Result
	err = json.Unmarshal(data, &result)
	return result, true, err
}

func resultSubject(result Result) (string, int64) {
	if result.Document != nil {
		return result.Document.Topic + "/" + result.Document.Key, result.Document.Version
	}
	if result.Proposal != nil {
		return strconv.FormatInt(result.Proposal.ID, 10), result.Proposal.Version
	}
	if result.Fact != nil {
		return result.Fact.Topic + "/" + result.Fact.Key, result.Fact.Version
	}
	return result.Memo.Key, result.Memo.Version
}

func mutate(ctx context.Context, tx pgx.Tx, actor string, c Command) (Result, error) {
	switch c.Name {
	case Curate, RemoveFact:
		return curate(ctx, tx, c)
	case Suggest:
		return suggest(ctx, tx, actor, c)
	case Review, assess:
		return decide(ctx, tx, actor, c)
	case DocumentSet, DocumentDelete:
		return writeDocument(ctx, tx, actor, c)
	case MemoSet, MemoDelete:
		return writeMemo(ctx, tx, actor, c)
	default:
		return Result{}, invalid()
	}
}
