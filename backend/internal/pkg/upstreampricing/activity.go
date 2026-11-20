package upstreampricing

import (
	"context"
	"time"
)

func (m *Manager) activityLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		m.activityCycle()
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) activityCycle() {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	ids, err := m.Store.activityDue(ctx, time.Now())
	cancel()
	if err != nil {
		return
	}
	for _, id := range ids {
		if m.ctx.Err() != nil {
			return
		}
		ctx, cancel = context.WithTimeout(m.ctx, 2*time.Minute)
		_ = m.syncActivity(ctx, id, time.Now())
		cancel()
	}
}

func (m *Manager) syncActivity(ctx context.Context, id int64, now time.Time) (err error) {
	token, err := m.Store.claimActivity(ctx, id, now)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			m.Store.failActivity(cleanup, id, token)
		}
	}()
	src, err := m.Store.Source(ctx, id)
	if err != nil {
		return err
	}
	if !src.HasCredential || src.ActiveVersion == nil {
		return safeError("请先保存上游定价的面板配置")
	}
	fp, err := m.Store.AccountFingerprint(ctx, id)
	if err != nil || fp != src.AccountFingerprint {
		return ErrConflict
	}
	c := src.Config
	if err = ValidateConfig(&c); err != nil {
		return err
	}
	identity := activityIdentity(src)
	c.Token, err = m.Cipher.Decrypt(src.Credential)
	if err != nil {
		message := "面板凭证无法解密，请重新填写并应用上游定价配置"
		return m.Store.publishActivity(ctx, src, token, identity, now, nil, nil, nil, message, message)
	}
	state, err := m.Store.activityState(ctx, id)
	if err != nil {
		return err
	}
	start, end := activityWindow(now, state.LastSuccess, state.Identity == identity)
	active, err := m.Store.activityHasCalls(ctx, id, start, end)
	if err != nil {
		return err
	}
	// Reserve time to publish the wallet snapshot even if log pagination times out.
	var balance *string
	walletError := ""
	value, walletErr := m.Remote.wallet(ctx, c)
	if walletErr == nil {
		balance = &value
	} else {
		message, _ := PublicMessage(walletErr)
		walletError = "钱包刷新失败：" + message
	}
	var records []TimingRecord
	var through *time.Time
	detailError := ""
	if active {
		// A bounded overlap absorbs station timestamp/recording lag. It never
		// crosses today's midnight and upserts make repeated rows idempotent.
		queryStart := start.Add(-3 * time.Minute)
		midnight, _ := activityWindow(now, nil, false)
		if queryStart.Before(midnight) {
			queryStart = midnight
		}
		detailCtx, cancel := context.WithTimeout(ctx, 80*time.Second)
		records, err = m.Remote.activityRecords(detailCtx, c, queryStart, end)
		cancel()
		if err == nil {
			through = &end
		} else {
			detailError = "明细同步失败，保留上次成功进度，下个周期重试"
			records = nil
		}
	}
	return m.Store.publishActivity(ctx, src, token, identity, now, through, records, balance, walletError, detailError)
}

func (m *Manager) Activity(ctx context.Context, id int64) (*ActivityState, error) {
	src, err := m.Store.Source(ctx, id)
	if err != nil {
		return nil, err
	}
	if !src.HasCredential || src.ActiveVersion == nil {
		return &ActivityState{}, nil
	}
	fp, err := m.Store.AccountFingerprint(ctx, id)
	if err != nil || fp != src.AccountFingerprint {
		return nil, ErrConflict
	}
	state, err := m.Store.activityState(ctx, id)
	if err != nil {
		return nil, err
	}
	if state.Identity != activityIdentity(src) {
		return &ActivityState{Configured: true, NextAttempt: state.NextAttempt, LastError: state.LastError}, nil
	}
	state.Configured = true
	return state, nil
}
