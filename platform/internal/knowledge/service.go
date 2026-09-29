package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/jackc/pgx/v5"

	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (s Service) Execute(ctx context.Context, actor string, command Command) (Result, error) {
	return s.execute(ctx, actor, command, false, nil, nil)
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
		Version: input.Version, Text: input.Reason, Decision: decision}, true, nil, nil)
}

func (s Service) execute(
	ctx context.Context,
	actor string,
	c Command,
	internal bool,
	sourceKeys []string,
	source *readsource.Derivation,
) (Result, error) {
	if err := validate(c, internal); err != nil {
		return Result{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	proposalSource, refs, err := knowledgeCommandSources(ctx, tx, c, source)
	if err != nil {
		return Result{}, err
	}
	if err = lockKnowledgeCommand(ctx, tx, actor, c, refs, source != nil || proposalSource.derived); err != nil {
		return Result{}, err
	}
	sources, err := resolveMemorySources(ctx, tx, actor, sourceKeys)
	if err != nil {
		return Result{}, err
	}
	encoded, err := knowledgeOperationBytes(c, sourceKeys, source)
	if err != nil {
		return Result{}, err
	}
	keyHash, requestHash := digest([]byte(c.Key)), digest(encoded)
	previous, found, err := replay(ctx, tx, actor, keyHash, requestHash)
	if err != nil {
		return Result{}, err
	}
	if found {
		if err = retainKnowledgeReplay(
			ctx,
			tx,
			actor,
			keyHash,
			&previous,
			refs,
			source,
			proposalSource.revoked,
		); err != nil {
			return Result{}, err
		}
		return previous, tx.Commit(ctx)
	}

	if err = lockKnowledgeEffect(ctx, tx, actor, source, proposalSource); err != nil {
		return Result{}, err
	}
	deletion, err := beginPrivateDeletion(ctx, tx, actor, c, source)
	if err != nil {
		return Result{}, err
	}
	result, err := mutate(ctx, tx, actor, c)
	if err != nil {
		return Result{}, err
	}
	if err = bindMemorySources(ctx, tx, actor, result, sources); err != nil {
		return Result{}, err
	}
	if err = bindKnowledgeResult(ctx, tx, actor, c, &result, source, refs, proposalSource); err != nil {
		return Result{}, err
	}
	if err = finishPrivateDeletion(ctx, tx, actor, deletion, &result); err != nil {
		return Result{}, err
	}
	if err = saveKnowledgeOperation(ctx, tx, actor, c, result, keyHash, requestHash); err != nil {
		return Result{}, err
	}
	return result, tx.Commit(ctx)
}

func saveKnowledgeOperation(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c Command,
	result Result,
	keyHash, requestHash string,
) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
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
		return err
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
		return err
	}
	return err
}

func lockActor(ctx context.Context, tx pgx.Tx, actor string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM core.users WHERE id=$1 FOR NO KEY UPDATE`, actor).Scan(&id)
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
	return knowledgeauthority.LockPermission(ctx, tx, actor, c.Event, permission)
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

// ExecuteDerived accepts source evidence only from the authenticated host path.
func (s Service) ExecuteDerived(
	ctx context.Context,
	actor string,
	c Command,
	source readsource.Derivation,
) (Result, error) {
	if !source.Valid() {
		return Result{}, invalid()
	}
	if _, err := readsource.Capture(actor, source); err != nil {
		return Result{}, err
	}
	source = source.Clone()
	return s.execute(ctx, actor, c, false, nil, &source)
}

func derivedSharedMemory(source readsource.Derivation) bool {
	for _, a := range source.Authorities {
		if a.Knowledge.Kind == knowledgeauthority.SharedMemory {
			return true
		}
		if a.Causal != nil {
			for _, leaf := range a.Causal.Authorities {
				if leaf.Knowledge.Kind == knowledgeauthority.SharedMemory {
					return true
				}
			}
		}
	}
	return false
}

func scrubMemoryResult(result *Result) {
	result.Redacted = true
	if result.Document != nil {
		result.Document.Text = ""
	}
	if result.Memo != nil {
		result.Memo.Text = ""
	}
	if result.Fact != nil {
		result.Fact.Text = ""
	}
	if result.Proposal != nil {
		result.Proposal.Text = ""
		result.Proposal.Reason = ""
	}
}

func bindKnowledgeResult(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c Command,
	result *Result,
	source *readsource.Derivation,
	refs []readsource.Authority,
	proposalSource proposalCausalRecord,
) error {
	var err error
	if source != nil {
		combined := source.Clone()
		combined.Authorities = refs
		if err = bindDerivedKnowledgeResult(ctx, tx, actor, c.Name, *result, combined); err != nil {
			return err
		}
	} else if c.Name == Review && c.Decision == approve {
		if err = publishProposalCausal(ctx, tx, actor, *result, proposalSource); err != nil {
			return err
		}
	}
	if proposalSource.derived && result.Proposal != nil {
		result.Proposal.ReadAuthorities, err = proposalReadAuthorities(*result.Proposal)
		if err != nil {
			return err
		}
	}
	return nil
}

func lockKnowledgeEffect(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	source *readsource.Derivation,
	proposal proposalCausalRecord,
) error {
	if source != nil {
		allowed, err := readsource.Lock(ctx, tx, actor, source.Authorities)
		if err != nil {
			return err
		}
		if slices.Contains(allowed, false) {
			return conflict("history_stale")
		}
		if err = fence.LockGeneration(ctx, tx, actor, source.Generation); err != nil {
			return err
		}
	}
	return lockProposalCausal(ctx, tx, actor, proposal)
}

func knowledgeCommandSources(
	ctx context.Context,
	tx pgx.Tx,
	c Command,
	source *readsource.Derivation,
) (proposalCausalRecord, []readsource.Authority, error) {
	var err error
	proposalSource := proposalCausalRecord{}
	refs := []readsource.Authority{}
	if source != nil {
		refs = source.Authorities
	}
	if c.Name == Review || c.Name == assess {
		proposalSource, err = loadProposalCausal(ctx, tx, c.ProposalID)
		if err != nil {
			return proposalSource, nil, err
		}
		refs, err = readsource.Merge(refs, proposalSource.refs)
		if err != nil {
			return proposalSource, nil, err
		}
	}
	refs, err = readsource.ExpandProposalSources(ctx, tx, refs)
	return proposalSource, refs, err
}

func retainKnowledgeReplay(
	ctx context.Context,
	tx pgx.Tx,
	actor, keyHash string,
	previous *Result,
	refs []readsource.Authority,
	source *readsource.Derivation,
	revoked bool,
) error {
	terminal, replayErr := authorizeMemoryReplay(ctx, tx, actor, previous, refs, source, revoked)
	if replayErr != nil {
		return replayErr
	}
	if terminal {
		if _, err := tx.Exec(
			ctx,
			`UPDATE core.knowledge_operations SET result=$3 WHERE actor=$1 AND key_hash=$2`,
			actor,
			keyHash,
			previous,
		); err != nil {
			return err
		}
	}
	return nil
}

func bindDerivedKnowledgeResult(
	ctx context.Context,
	tx pgx.Tx,
	actor, name string,
	result Result,
	source readsource.Derivation,
) error {
	if name == Review {
		return bindMemoryCausalRefsFromDerivation(ctx, tx, actor, result, source)
	}
	return bindMemoryCausal(ctx, tx, actor, result, source)
}
