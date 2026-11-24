package marketplace

import (
    "context"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
)

type OrderCompletionApproval struct {
    OrderID string `json:"order_id"`
    ApprovedAt time.Time `json:"approved_at"`
}

func (s *Store) ApproveOrderCompletion(ctx context.Context, actor Actor, orderID string) (OrderCompletionApproval, bool, error) {
    if actor.Role != RoleCustomer || actor.SubjectID == "" || actor.TenantID == "" || orderID == "" {
        return OrderCompletionApproval{}, false, ErrForbidden
    }
    tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
    if err != nil { return OrderCompletionApproval{}, false, err }
    defer tx.Rollback(ctx)

    var customerID string
    var tenantID *string
    var status string
    err = tx.QueryRow(ctx, "SELECT customer_id, tenant_id, status FROM orders WHERE id = $1 FOR UPDATE", orderID).Scan(&customerID, &tenantID, &status)
    if errors.Is(err, pgx.ErrNoRows) { return OrderCompletionApproval{}, false, ErrOrderNotFound }
    if err != nil { return OrderCompletionApproval{}, false, err }
    if tenantID == nil || Authorize(actor, Resource{TenantID: *tenantID, OwnerID: customerID}, ActionOrderApproveCompletion) != nil {
        return OrderCompletionApproval{}, false, ErrForbidden
    }

    var existing OrderCompletionApproval
    err = tx.QueryRow(ctx, "SELECT order_id, approved_at FROM order_completion_approvals WHERE order_id = $1", orderID).Scan(&existing.OrderID, &existing.ApprovedAt)
    if err == nil {
        if err = tx.Commit(ctx); err != nil { return OrderCompletionApproval{}, false, err }
        return existing, false, nil
    }
    if !errors.Is(err, pgx.ErrNoRows) { return OrderCompletionApproval{}, false, err }
    if status != "completed" { return OrderCompletionApproval{}, false, ErrInvalidTransition }

    var approval OrderCompletionApproval
    err = tx.QueryRow(ctx, "INSERT INTO order_completion_approvals(order_id, customer_identity_id) VALUES ($1, $2) RETURNING order_id, approved_at", orderID, actor.SubjectID).Scan(&approval.OrderID, &approval.ApprovedAt)
    if err != nil { return OrderCompletionApproval{}, false, err }
    if err = tx.Commit(ctx); err != nil { return OrderCompletionApproval{}, false, err }
    return approval, true, nil
}

func loadOrderCompletionApproval(ctx context.Context, tx pgx.Tx, orderID string) (*OrderCompletionApproval, error) {
    var approval OrderCompletionApproval
    err := tx.QueryRow(ctx, "SELECT order_id, approved_at FROM order_completion_approvals WHERE order_id = $1", orderID).Scan(&approval.OrderID, &approval.ApprovedAt)
    if errors.Is(err, pgx.ErrNoRows) { return nil, nil }
    if err != nil { return nil, err }
    return &approval, nil
}
