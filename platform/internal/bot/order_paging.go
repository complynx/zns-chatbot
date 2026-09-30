package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const (
	orderPageSize    = 10
	orderPagePrefix  = "o:page:"
	ownOrdersScope   = "orders"
	adminOrdersScope = "admin"
	orderPagerPrefix = "paging:"
)

func boundedOrderPage(page, total int) int { return min(max(page, 0), max(total-1, 0)/orderPageSize) }

func (b *Bot) saveOrderPage(ctx context.Context, owner, scope string, page int, update int64) error {
	_, err := b.DB.Exec(ctx, `INSERT INTO bot.order_pages(owner,scope,page,last_update,event_id) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(owner,scope,event_id) DO UPDATE SET page=$3,last_update=greatest(bot.order_pages.last_update,$4)
 WHERE $4=0 OR bot.order_pages.last_update <= $4`, owner, scope, page, update, b.currentOrderEvent())
	return core.DatabaseOperationError(err)
}

func (b *Bot) currentOrderPage(ctx context.Context, owner, scope string, total int) (int, error) {
	page := 0
	err := b.DB.QueryRow(ctx, `SELECT page FROM bot.order_pages WHERE owner=$1 AND scope=$2 AND event_id=$3`, owner, scope, b.currentOrderEvent()).
		Scan(&page)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, core.DatabaseOperationError(err)
	}
	bounded := boundedOrderPage(page, total)
	if bounded != page {
		err = b.saveOrderPage(ctx, owner, scope, bounded, 0)
	} else {
		err = nil
	}
	return bounded, err
}

func (b *Bot) orderPageButton(ctx context.Context, owner, scope, label string, page int) (telegram.Button, error) {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d/%s", owner, scope, page, b.currentOrderEvent())))
	token := hex.EncodeToString(sum[:16])
	_, err := b.DB.Exec(
		ctx,
		`INSERT INTO bot.order_page_buttons(owner,token,scope,page,event_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
		owner,
		token,
		scope,
		page,
		b.currentOrderEvent(),
	)
	return telegram.Button{Text: label, Data: orderPagePrefix + token}, core.DatabaseOperationError(err)
}

func (b *Bot) handleOrderPage(ctx context.Context, in incoming, update int64) (string, error) {
	var scope string
	var page int
	err := b.DB.QueryRow(ctx, `SELECT scope,page FROM bot.order_page_buttons WHERE owner=$1 AND token=$2`, in.owner, strings.TrimPrefix(in.text, orderPagePrefix)).
		Scan(&scope, &page)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	preference, preferenceErr := b.API.Preferences(ctx, in.owner)
	if preferenceErr != nil {
		return "", preferenceErr
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return i18n.Translate(preference.Language, i18n.OrderPageUnavailable, nil)
	}
	list, err := b.orderPageList(ctx, in.owner, scope)
	if core.IsDatabaseFailure(err) {
		return "", core.ErrDatabase
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status == http.StatusForbidden {
		return i18n.Translate(preference.Language, i18n.OrderPageUnavailable, nil)
	}
	if err != nil {
		return "", err
	}
	page = boundedOrderPage(page, len(list))
	if err = b.saveOrderPage(ctx, in.owner, scope, page, update); err != nil {
		return "", err
	}
	page, err = b.currentOrderPage(ctx, in.owner, scope, len(list))
	if err != nil {
		return "", err
	}
	return orderPageText(preference.Language, scope, page, len(list))
}

func (b *Bot) orderPageList(ctx context.Context, owner, scope string) ([]orders.Order, error) {
	if scope == adminOrdersScope {
		return b.API.PaymentInbox(ctx, owner, b.currentOrderEvent())
	}
	return b.API.Orders(ctx, owner, b.currentOrderEvent())
}

// A successful explicit action brings its order into view, including an agent
// action that names an order outside the current page. Core remains authoritative.
func (b *Bot) focusOrderPage(ctx context.Context, owner string, order orders.Order, update int64) error {
	scope := ownOrdersScope
	if order.Owner != owner {
		scope = adminOrdersScope
	}
	list, err := b.orderPageList(ctx, owner, scope)
	if err != nil {
		return err
	}
	for index, item := range list {
		if item.ID == order.ID {
			return b.saveOrderPage(ctx, owner, scope, index/orderPageSize, update)
		}
	}
	return nil
}

func (b *Bot) pagedOrders(
	ctx context.Context,
	owner string,
	chat int64,
	scope, language string,
	list []orders.Order,
	active, available map[string]bool,
) ([]orders.Order, error) {
	for _, order := range list {
		key := order.ID
		if scope == adminOrdersScope {
			key = "admin:" + key
		} else {
			available[paymentCardPrefix+key] = true
		}
		available[key] = true
	}
	page, err := b.currentOrderPage(ctx, owner, scope, len(list))
	if err != nil {
		return nil, err
	}
	key := orderPagerPrefix + scope
	var opened bool
	if err = b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.order_cards WHERE owner=$1 AND card_key=$2)`, owner, key).
		Scan(&opened); err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	if len(list) > orderPageSize || opened {
		active[key] = true
		if err = b.renderOrderPager(ctx, owner, chat, scope, language, page, len(list)); err != nil {
			return nil, err
		}
	}
	start := min(page*orderPageSize, len(list))
	return list[start:min(start+orderPageSize, len(list))], nil
}

func orderPageText(language, scope string, page, total int) (string, error) {
	id := i18n.OrderPage
	if scope == adminOrdersScope {
		id = i18n.PaymentInboxPage
	}
	return i18n.Translate(
		language,
		id,
		map[string]string{
			"page":        strconv.Itoa(page + 1),
			"pages":       strconv.Itoa(max(total-1, 0)/orderPageSize + 1),
			orderTotalKey: strconv.Itoa(total),
		},
	)
}

func (b *Bot) renderOrderPager(
	ctx context.Context,
	owner string,
	chat int64,
	scope, language string,
	page, total int,
) error {
	text, err := orderPageText(language, scope, page, total)
	if err != nil {
		return err
	}
	row := []telegram.Button{}
	for _, direction := range []struct {
		page  int
		label i18n.ID
	}{{page - 1, i18n.PagePrevious}, {page + 1, i18n.PageNext}} {
		if direction.page < 0 || direction.page > boundedOrderPage(direction.page, total) {
			continue
		}
		label, labelErr := i18n.Translate(language, direction.label, nil)
		if labelErr != nil {
			return labelErr
		}
		button, buttonErr := b.orderPageButton(ctx, owner, scope, label, direction.page)
		if buttonErr != nil {
			return buttonErr
		}
		row = append(row, button)
	}
	rows := [][]telegram.Button{}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return b.deliverOrderCard(
		ctx,
		owner,
		orderPagerPrefix+scope,
		telegram.Send{ChatID: chat, Text: text, Markup: telegram.Markup{Rows: rows}},
	)
}
