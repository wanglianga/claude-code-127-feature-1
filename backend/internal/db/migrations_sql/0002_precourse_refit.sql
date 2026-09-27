-- 课程前尺码重配（教练发现初学者雪鞋过松等安全隐患时发起）
-- 流程：教练发起 → 柜台按库存推荐并换发 → 教练重新确认安全动作与课程时间 → 完成

-- 课程因重配累计顺延的分钟数（教练排课同步调整，游客/教练端可见）
ALTER TABLE courses ADD COLUMN shift_minutes INT NOT NULL DEFAULT 0;

CREATE TABLE refits (
    id               BIGSERIAL PRIMARY KEY,
    rental_id        BIGINT NOT NULL REFERENCES rentals(id),
    rental_item_id   BIGINT NOT NULL REFERENCES rental_items(id),
    course_id        BIGINT REFERENCES courses(id),
    reason           TEXT NOT NULL,                        -- 调换原因（教练填写）
    old_size         TEXT NOT NULL DEFAULT '',             -- 原尺码
    new_size         TEXT NOT NULL DEFAULT '',             -- 柜台换发后的新尺码
    old_item_id      BIGINT REFERENCES equipment_items(id),
    new_item_id      BIGINT REFERENCES equipment_items(id),
    charge_fee       NUMERIC(10,2) NOT NULL DEFAULT 0,     -- 重配收费（安全重配免费）
    compensate_amount NUMERIC(10,2) NOT NULL DEFAULT 0,    -- 游客补偿（租金减免）
    delay_minutes    INT NOT NULL DEFAULT 0,               -- 本次重配耗时（影响课程开始时间）
    shift_applied    INT NOT NULL DEFAULT 0,               -- 实际计入课程顺延的分钟数
    rights_impact    TEXT NOT NULL DEFAULT '',             -- 课程权益影响说明（游客端可见）
    status           TEXT NOT NULL DEFAULT 'requested'
                     CHECK (status IN ('requested','awaiting_confirm','done','cancelled')),
    requested_by     BIGINT NOT NULL REFERENCES users(id), -- 发起教练
    processed_by     BIGINT REFERENCES users(id),          -- 处理柜台
    reconfirmed_by   BIGINT REFERENCES users(id),          -- 复核教练
    reconfirm_note   TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at     TIMESTAMPTZ,
    reconfirmed_at   TIMESTAMPTZ
);
CREATE INDEX idx_refits_rental ON refits(rental_id);
CREATE INDEX idx_refits_status ON refits(status);
CREATE INDEX idx_refits_course ON refits(course_id);

-- 游客补偿（平台租金减免）累计在租赁单上，与租金合计分开列示
ALTER TABLE rentals ADD COLUMN compensate_total NUMERIC(10,2) NOT NULL DEFAULT 0;

-- 押金流水新增类型：课前重配延误补偿（随归还结算以租金减免形式返还）
ALTER TABLE deposit_transactions DROP CONSTRAINT IF EXISTS deposit_transactions_type_check;
ALTER TABLE deposit_transactions ADD CONSTRAINT deposit_transactions_type_check
    CHECK (type IN ('hold','deduct_damage','deduct_lost','deduct_late',
                    'insurance_offset','refit_compensation','refund'));
