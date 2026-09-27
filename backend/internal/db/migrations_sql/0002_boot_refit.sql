-- 课程前尺码重配（教练发现初学者雪鞋过松影响安全时发起）
-- 1) 队列增加 refit 类型（课前重配独立排队）
ALTER TABLE queue_tickets DROP CONSTRAINT queue_tickets_queue_type_check;
ALTER TABLE queue_tickets ADD CONSTRAINT queue_tickets_queue_type_check
    CHECK (queue_type IN ('pickup','exchange','return_day','return_night','damage','refit'));

-- 2) 押金流水增加 compensation 类型（课程延误补偿，租金减免留痕）
ALTER TABLE deposit_transactions DROP CONSTRAINT deposit_transactions_type_check;
ALTER TABLE deposit_transactions ADD CONSTRAINT deposit_transactions_type_check
    CHECK (type IN ('hold','deduct_damage','deduct_lost','deduct_late','insurance_offset','refund','compensation'));

-- 3) 课前重配单
CREATE TABLE refits (
    id                BIGSERIAL PRIMARY KEY,
    rental_item_id    BIGINT NOT NULL REFERENCES rental_items(id), -- 被重配的雪鞋明细
    rental_id         BIGINT NOT NULL REFERENCES rentals(id),
    course_id         BIGINT REFERENCES courses(id),
    guest_id          BIGINT NOT NULL REFERENCES users(id),
    coach_id          BIGINT NOT NULL REFERENCES users(id),        -- 发起教练
    reason            TEXT NOT NULL,                               -- 调换原因（安全）
    old_item_id       BIGINT REFERENCES equipment_items(id),
    old_size          TEXT NOT NULL,
    target_size       TEXT NOT NULL,                               -- 系统按脚码推算的目标尺码
    target_eu         NUMERIC(4,1),                                -- 目标欧码数值（库存推荐用）
    new_item_id       BIGINT REFERENCES equipment_items(id),
    new_size          TEXT,
    est_minutes       INT NOT NULL DEFAULT 0,                      -- 发起时预估耗时
    actual_minutes    INT,                                         -- 柜台完成时登记的实际耗时
    course_start_at   TIMESTAMPTZ,                                 -- 原课程开始时间（快照）
    course_new_start_at TIMESTAMPTZ,                               -- 顺延后的课程开始时间
    delay_minutes     INT NOT NULL DEFAULT 0,                      -- 课程开始顺延分钟
    compensate_yuan   NUMERIC(10,2) NOT NULL DEFAULT 0,            -- 游客补偿（租金减免）
    charge_yuan       NUMERIC(10,2) NOT NULL DEFAULT 0,            -- 是否收费（安全重配免费=0）
    rights_impact     TEXT NOT NULL DEFAULT '',                    -- 课程权益影响说明（游客端可见）
    status            TEXT NOT NULL DEFAULT 'requested'
                      CHECK (status IN ('requested','done','confirmed','cancelled')),
    -- requested: 教练已发起，柜台处理中
    -- done:      柜台已换发新雪鞋，等教练重新确认安全动作与课程时间
    -- confirmed: 教练已重新确认，流程闭环
    -- cancelled: 已取消
    processed_by      BIGINT REFERENCES users(id),                 -- 柜台
    coach_reconfirm_note TEXT NOT NULL DEFAULT '',                 -- 教练二次确认备注
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at      TIMESTAMPTZ,
    confirmed_at      TIMESTAMPTZ
);
CREATE INDEX idx_refits_rental ON refits(rental_id);
CREATE INDEX idx_refits_status ON refits(status);
CREATE INDEX idx_refits_coach ON refits(coach_id);
