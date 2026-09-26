-- 滑雪场雪具租赁平台 初始化结构
-- 金额单位：人民币元

CREATE TABLE users (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    phone           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    role            TEXT NOT NULL CHECK (role IN ('guest','coach','counter','repair','insurance','finance','admin')),
    -- 游客体测信息（其他角色留空）
    height_cm       INT,
    weight_kg       INT,
    foot_eu         NUMERIC(4,1),
    head_cm         INT,
    level           TEXT CHECK (level IS NULL OR level IN ('beginner','intermediate','advanced')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE teams (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    leader_id   BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE team_members (
    team_id     BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    guest_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, guest_id)
);

-- 雪具型号（同型号同尺码的一批雪具）
CREATE TABLE equipment_models (
    id              BIGSERIAL PRIMARY KEY,
    code            TEXT NOT NULL UNIQUE,
    brand           TEXT NOT NULL,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL CHECK (type IN ('ski','boot','helmet','pole','protector')),
    size_label      TEXT NOT NULL,                 -- 如 150cm / EU42 / M / 110cm
    spec            JSONB NOT NULL DEFAULT '{}',   -- 类型相关参数：雪板身高/体重/水平区间等
    rental_fee      NUMERIC(10,2) NOT NULL,        -- 单日租金
    deposit         NUMERIC(10,2) NOT NULL,        -- 单件押金
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 单件雪具（每件一个二维码）
CREATE TABLE equipment_items (
    id              BIGSERIAL PRIMARY KEY,
    model_id        BIGINT NOT NULL REFERENCES equipment_models(id),
    qr_code         TEXT NOT NULL UNIQUE,
    serial_no       TEXT,
    status          TEXT NOT NULL DEFAULT 'available'
                    CHECK (status IN ('available','reserved','out','inspection','repair','retired')),
    condition_note  TEXT NOT NULL DEFAULT '',
    rental_count    INT NOT NULL DEFAULT 0,
    current_rental_item_id BIGINT,
    purchased_at    DATE NOT NULL DEFAULT CURRENT_DATE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_items_model ON equipment_items(model_id);
CREATE INDEX idx_items_status ON equipment_items(status);

-- 课程
CREATE TABLE courses (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    coach_id      BIGINT REFERENCES users(id),
    session_type  TEXT NOT NULL CHECK (session_type IN ('day','night')),
    start_at      TIMESTAMPTZ NOT NULL,
    end_at        TIMESTAMPTZ NOT NULL,
    level         TEXT NOT NULL DEFAULT 'beginner' CHECK (level IN ('beginner','intermediate','advanced')),
    capacity      INT NOT NULL,
    booked_count  INT NOT NULL DEFAULT 0,
    meeting_point TEXT NOT NULL DEFAULT '雪具大厅集合点'
);
CREATE INDEX idx_courses_start ON courses(start_at);

-- 分时段取具预约槽（旺季减少排队）
CREATE TABLE pickup_slots (
    id            BIGSERIAL PRIMARY KEY,
    slot_date     DATE NOT NULL,
    start_time    TEXT NOT NULL,   -- '08:30'
    end_time      TEXT NOT NULL,
    session_type  TEXT NOT NULL CHECK (session_type IN ('day','night')),
    capacity      INT NOT NULL,
    booked_count  INT NOT NULL DEFAULT 0,
    UNIQUE (slot_date, start_time, session_type)
);

-- 租赁单（一个游客/团队一次租赁）
CREATE TABLE rentals (
    id              BIGSERIAL PRIMARY KEY,
    code            TEXT NOT NULL UNIQUE,
    guest_id        BIGINT NOT NULL REFERENCES users(id),
    team_id         BIGINT REFERENCES teams(id),
    course_id       BIGINT REFERENCES courses(id),
    slot_id         BIGINT REFERENCES pickup_slots(id),
    ticket_no       TEXT,                                  -- 购票/报名凭证号
    session_type    TEXT NOT NULL CHECK (session_type IN ('day','night')),
    status          TEXT NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft','reserved','equipped','return_pending','returned','cancelled')),
    deposit_total   NUMERIC(10,2) NOT NULL DEFAULT 0,
    fee_total       NUMERIC(10,2) NOT NULL DEFAULT 0,
    premium_total   NUMERIC(10,2) NOT NULL DEFAULT 0,
    extra_hours     NUMERIC(4,1) NOT NULL DEFAULT 0,      -- 课程延时
    pickup_at       TIMESTAMPTZ,
    return_due_at   TIMESTAMPTZ,
    returned_at     TIMESTAMPTZ,
    night_return    BOOLEAN NOT NULL DEFAULT FALSE,       -- 是否走夜场归还独立排队
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_rentals_guest ON rentals(guest_id);
CREATE INDEX idx_rentals_status ON rentals(status);

-- 租赁明细（建议 + 实际分配的单件雪具）
CREATE TABLE rental_items (
    id                BIGSERIAL PRIMARY KEY,
    rental_id         BIGINT NOT NULL REFERENCES rentals(id) ON DELETE CASCADE,
    equipment_type    TEXT NOT NULL CHECK (equipment_type IN ('ski','boot','helmet','pole','protector')),
    model_id          BIGINT REFERENCES equipment_models(id),
    item_id           BIGINT REFERENCES equipment_items(id),
    recommended_size  TEXT NOT NULL,
    reason            TEXT NOT NULL DEFAULT '',
    fee               NUMERIC(10,2) NOT NULL DEFAULT 0,
    deposit           NUMERIC(10,2) NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'suggested'
                      CHECK (status IN ('suggested','assigned','out','exchanged','returned_ok','returned_damaged','repair','lost')),
    -- 教练课前确认
    fit_status        TEXT NOT NULL DEFAULT 'pending' CHECK (fit_status IN ('pending','ok','issue')),
    fit_note          TEXT NOT NULL DEFAULT '',
    fit_checked_by    BIGINT REFERENCES users(id),
    fit_checked_at    TIMESTAMPTZ,
    exchanged_from_item_id BIGINT REFERENCES equipment_items(id),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    returned_at       TIMESTAMPTZ
);
CREATE INDEX idx_rental_items_rental ON rental_items(rental_id);
CREATE INDEX idx_rental_items_item ON rental_items(item_id);

-- 排队叫号（取具 / 换码 / 日场归还 / 夜场归还 / 损坏处理 分开）
CREATE TABLE queue_tickets (
    id            BIGSERIAL PRIMARY KEY,
    rental_id     BIGINT NOT NULL REFERENCES rentals(id),
    queue_type    TEXT NOT NULL CHECK (queue_type IN ('pickup','exchange','return_day','return_night','damage')),
    status        TEXT NOT NULL DEFAULT 'waiting'
                  CHECK (status IN ('waiting','called','serving','done','cancelled')),
    priority      INT NOT NULL DEFAULT 0,          -- 团队优先等
    ticket_no     TEXT NOT NULL,
    station       TEXT,
    called_by     BIGINT REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    called_at     TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ
);
CREATE INDEX idx_queue_live ON queue_tickets(queue_type, status, created_at);

-- 换码记录
CREATE TABLE exchanges (
    id              BIGSERIAL PRIMARY KEY,
    rental_item_id  BIGINT NOT NULL REFERENCES rental_items(id),
    old_item_id     BIGINT REFERENCES equipment_items(id),
    new_item_id     BIGINT REFERENCES equipment_items(id),
    new_size        TEXT NOT NULL,
    reason          TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'requested'
                    CHECK (status IN ('requested','approved','done','rejected')),
    requested_by    BIGINT NOT NULL REFERENCES users(id),
    processed_by    BIGINT REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at    TIMESTAMPTZ
);

-- 课程延时
CREATE TABLE extensions (
    id          BIGSERIAL PRIMARY KEY,
    rental_id   BIGINT NOT NULL REFERENCES rentals(id),
    hours       NUMERIC(4,1) NOT NULL,
    fee         NUMERIC(10,2) NOT NULL,
    created_by  BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 损坏 / 异常报告
CREATE TABLE damage_reports (
    id            BIGSERIAL PRIMARY KEY,
    rental_item_id BIGINT NOT NULL REFERENCES rental_items(id),
    reporter_id   BIGINT NOT NULL REFERENCES users(id),
    stage         TEXT NOT NULL CHECK (stage IN ('during_rental','at_return')),
    damage_type   TEXT NOT NULL CHECK (damage_type IN
                  ('edge_damage','base_scratch','binding_issue','liner_wet','shell_crack','strap_broken','lost','other')),
    severity      TEXT NOT NULL CHECK (severity IN ('light','medium','heavy','lost')),
    description   TEXT NOT NULL DEFAULT '',
    photos        JSONB NOT NULL DEFAULT '[]',
    status        TEXT NOT NULL DEFAULT 'reported'
                  CHECK (status IN ('reported','assessed','repair_created','claim_filed','closed')),
    deduction     NUMERIC(10,2) NOT NULL DEFAULT 0,
    assessed_by   BIGINT REFERENCES users(id),
    repair_order_id BIGINT,
    claim_id      BIGINT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_damage_item ON damage_reports(rental_item_id);

-- 维修工单
CREATE TABLE repair_orders (
    id            BIGSERIAL PRIMARY KEY,
    item_id       BIGINT NOT NULL REFERENCES equipment_items(id),
    damage_id     BIGINT REFERENCES damage_reports(id),
    issue         TEXT NOT NULL,
    advice        TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'queued'
                  CHECK (status IN ('queued','in_progress','waiting_part','done','scrapped')),
    repair_cost   NUMERIC(10,2) NOT NULL DEFAULT 0,
    technician_id BIGINT REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ
);
CREATE INDEX idx_repair_status ON repair_orders(status);

-- 高损耗型号保养提醒
CREATE TABLE maintenance_reminders (
    id            BIGSERIAL PRIMARY KEY,
    model_id      BIGINT NOT NULL REFERENCES equipment_models(id),
    reason        TEXT NOT NULL,
    metrics       JSONB NOT NULL DEFAULT '{}',
    action        TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','done','dismissed')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_by   BIGINT REFERENCES users(id),
    resolved_at   TIMESTAMPTZ
);

-- 保险套餐
CREATE TABLE insurance_products (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    premium       NUMERIC(10,2) NOT NULL,        -- 保费
    coverage_cap  NUMERIC(10,2) NOT NULL,        -- 最高赔付
    deductible    NUMERIC(10,2) NOT NULL,        -- 免赔额
    covers        TEXT[] NOT NULL DEFAULT '{}',  -- 覆盖的 damage_type
    session_type  TEXT NOT NULL DEFAULT 'both' CHECK (session_type IN ('day','night','both')),
    active        BOOLEAN NOT NULL DEFAULT TRUE
);

-- 保单
CREATE TABLE insurance_policies (
    id          BIGSERIAL PRIMARY KEY,
    rental_id   BIGINT NOT NULL REFERENCES rentals(id),
    product_id  BIGINT NOT NULL REFERENCES insurance_products(id),
    premium     NUMERIC(10,2) NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','claimed','closed')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 理赔
CREATE TABLE insurance_claims (
    id               BIGSERIAL PRIMARY KEY,
    policy_id        BIGINT NOT NULL REFERENCES insurance_policies(id),
    damage_id        BIGINT REFERENCES damage_reports(id),
    amount_requested NUMERIC(10,2) NOT NULL,
    deductible       NUMERIC(10,2) NOT NULL DEFAULT 0,
    amount_approved  NUMERIC(10,2) NOT NULL DEFAULT 0,
    payout           NUMERIC(10,2) NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'filed'
                     CHECK (status IN ('filed','under_review','approved','rejected','paid')),
    adjuster_id      BIGINT REFERENCES users(id),
    note             TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE damage_reports ADD CONSTRAINT fk_damage_claim
    FOREIGN KEY (claim_id) REFERENCES insurance_claims(id) DEFERRABLE INITIALLY IMMEDIATE;

-- 押金流水（冻结/扣减/保险抵扣/退还）
CREATE TABLE deposit_transactions (
    id          BIGSERIAL PRIMARY KEY,
    rental_id   BIGINT NOT NULL REFERENCES rentals(id),
    type        TEXT NOT NULL CHECK (type IN ('hold','deduct_damage','deduct_lost','deduct_late','insurance_offset','refund')),
    amount      NUMERIC(10,2) NOT NULL,   -- 正数
    damage_id   BIGINT REFERENCES damage_reports(id),
    claim_id    BIGINT REFERENCES insurance_claims(id),
    note        TEXT NOT NULL DEFAULT '',
    created_by  BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_deposit_rental ON deposit_transactions(rental_id);

-- 安全提醒（初学者 / 反复换码游客等）
CREATE TABLE safety_reminders (
    id          BIGSERIAL PRIMARY KEY,
    guest_id    BIGINT NOT NULL REFERENCES users(id),
    rental_id   BIGINT REFERENCES rentals(id),
    message     TEXT NOT NULL,
    read_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 归还反馈
CREATE TABLE feedback (
    id          BIGSERIAL PRIMARY KEY,
    rental_id   BIGINT NOT NULL REFERENCES rentals(id),
    guest_id    BIGINT NOT NULL REFERENCES users(id),
    rating      INT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    text        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
