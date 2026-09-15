-- 养老助餐配送餐盒回收与补贴核销平台 - 数据库结构
-- 所有金额单位：元；日期时间均使用数据库时区

CREATE TABLE IF NOT EXISTS users (
    id            SERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name          TEXT NOT NULL,
    phone         TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL CHECK (role IN ('admin','finance','community','kitchen','rider','volunteer','family','elder')),
    active        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 老人档案：身份、补贴资格、饮食禁忌、送餐地址、敲门确认、餐盒回收方式、紧急联系人、特殊照护标记
CREATE TABLE IF NOT EXISTS elders (
    id                      SERIAL PRIMARY KEY,
    user_id                 INT REFERENCES users(id),          -- 老人本人账号（会用手机的老人才有）
    name                    TEXT NOT NULL,
    id_card                 TEXT NOT NULL UNIQUE,
    gender                  TEXT NOT NULL DEFAULT '女',
    birth_date              DATE,
    phone                   TEXT NOT NULL DEFAULT '',
    address                 TEXT NOT NULL,                     -- 送餐地址
    subsidy_level           TEXT NOT NULL DEFAULT 'none' CHECK (subsidy_level IN ('none','partial','full')),
    subsidy_per_meal        NUMERIC(10,2) NOT NULL DEFAULT 0,  -- 每餐补贴金额（资格快照会复制到餐单）
    dietary_restrictions    TEXT NOT NULL DEFAULT '',          -- 饮食禁忌，如 低盐;低糖;软烂;忌海鲜
    need_knock_confirm      BOOLEAN NOT NULL DEFAULT FALSE,    -- 是否需要敲门确认
    box_return_method       TEXT NOT NULL DEFAULT 'next_delivery' CHECK (box_return_method IN ('next_delivery','community_point','onsite')),
    emergency_contact_name  TEXT NOT NULL DEFAULT '',
    emergency_contact_phone TEXT NOT NULL DEFAULT '',
    cognitive_impairment    BOOLEAN NOT NULL DEFAULT FALSE,    -- 认知障碍
    living_alone            BOOLEAN NOT NULL DEFAULT FALSE,    -- 独居
    mobility_impaired       BOOLEAN NOT NULL DEFAULT FALSE,    -- 行动不便
    risk_level              TEXT NOT NULL DEFAULT 'normal' CHECK (risk_level IN ('normal','attention','high')), -- 风险标签：正常/关注/高风险
    delivery_confirm_mode   TEXT NOT NULL DEFAULT 'direct' CHECK (delivery_confirm_mode IN ('direct','phone_first')), -- 配送方式：直接上门/电话确认后再上门
    no_answer_count         INT NOT NULL DEFAULT 0,            -- 连续未开门次数（成功送达或回访后清零）
    focus_until             DATE,                              -- 重点关注截止日（次日重点关注）
    family_user_id          INT REFERENCES users(id),          -- 绑定家属账号
    community_note          TEXT NOT NULL DEFAULT '',
    active                  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 菜品库：营养与适口性标记（低盐/低糖/软烂程度）
CREATE TABLE IF NOT EXISTS dishes (
    id           SERIAL PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    price        NUMERIC(10,2) NOT NULL,
    low_salt     BOOLEAN NOT NULL DEFAULT FALSE,
    low_sugar    BOOLEAN NOT NULL DEFAULT FALSE,
    softness     TEXT NOT NULL DEFAULT 'normal' CHECK (softness IN ('normal','soft','mushy')), -- 普通/软/软烂
    nutrition    TEXT NOT NULL DEFAULT '',   -- 营养说明：热量、蛋白质等
    allergens    TEXT NOT NULL DEFAULT '',
    holiday_only BOOLEAN NOT NULL DEFAULT FALSE, -- 节日特供
    available    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 餐单（订单）：同一餐单贯穿 老人/家属/社区/厨房/骑手/财政核销
CREATE TABLE IF NOT EXISTS orders (
    id                 SERIAL PRIMARY KEY,
    order_no           TEXT NOT NULL UNIQUE,
    elder_id           INT NOT NULL REFERENCES elders(id),
    created_by         INT NOT NULL REFERENCES users(id),
    order_source       TEXT NOT NULL CHECK (order_source IN ('self','family','community')), -- 老人自订/家属代订/社区代订
    meal_date          DATE NOT NULL,
    meal_type          TEXT NOT NULL DEFAULT 'lunch' CHECK (meal_type IN ('breakfast','lunch','dinner')),
    delivery_type      TEXT NOT NULL DEFAULT 'home' CHECK (delivery_type IN ('home','community_pickup','volunteer')), -- 配送到家/社区食堂自取/志愿者帮送
    address            TEXT NOT NULL DEFAULT '',     -- 送餐地址快照
    need_knock_confirm BOOLEAN NOT NULL DEFAULT FALSE,
    strict_mode        BOOLEAN NOT NULL DEFAULT FALSE, -- 认知障碍/独居/行动不便 => 签收更严格
    box_return_method  TEXT NOT NULL DEFAULT 'next_delivery',
    boxes_issued       INT NOT NULL DEFAULT 2,
    total_amount       NUMERIC(10,2) NOT NULL DEFAULT 0,
    subsidy_amount     NUMERIC(10,2) NOT NULL DEFAULT 0,
    holiday_extra      NUMERIC(10,2) NOT NULL DEFAULT 0, -- 节日加餐额外补贴
    payable_amount     NUMERIC(10,2) NOT NULL DEFAULT 0,
    refund_amount      NUMERIC(10,2) NOT NULL DEFAULT 0,
    is_holiday_special BOOLEAN NOT NULL DEFAULT FALSE,
    holiday_name       TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'pending' CHECK (status IN
        ('pending','confirmed','preparing','ready','delivering','signed','completed','exception','cancelled','refunded','settled')),
    notes              TEXT NOT NULL DEFAULT '',
    cancel_reason      TEXT NOT NULL DEFAULT '',
    batch_id           INT,
    settled_in         INT,                            -- 所属核销批次（财政档案）
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS order_items (
    id         SERIAL PRIMARY KEY,
    order_id   INT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    dish_id    INT REFERENCES dishes(id),
    dish_name  TEXT NOT NULL,   -- 快照
    price      NUMERIC(10,2) NOT NULL,
    qty        INT NOT NULL DEFAULT 1,
    custom_note TEXT NOT NULL DEFAULT ''  -- 个性化：如 少盐、剪碎
);

-- 厨房出餐批次
CREATE TABLE IF NOT EXISTS kitchen_batches (
    id          SERIAL PRIMARY KEY,
    batch_date  DATE NOT NULL,
    meal_type   TEXT NOT NULL DEFAULT 'lunch',
    batch_no    TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL DEFAULT 'preparing' CHECK (status IN ('preparing','released')),
    released_at TIMESTAMPTZ,
    operator_id INT REFERENCES users(id),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (batch_date, meal_type)
);

-- 批次内每道菜 计划/实际 出餐数量（实际 < 计划 => 厨房少做异常）
CREATE TABLE IF NOT EXISTS batch_items (
    id          SERIAL PRIMARY KEY,
    batch_id    INT NOT NULL REFERENCES kitchen_batches(id) ON DELETE CASCADE,
    dish_id     INT REFERENCES dishes(id),
    dish_name   TEXT NOT NULL,
    planned_qty INT NOT NULL DEFAULT 0,
    actual_qty  INT NOT NULL DEFAULT 0,
    UNIQUE (batch_id, dish_name)
);

-- 配送记录：保温箱、路线、送达照片、签收、异常
CREATE TABLE IF NOT EXISTS deliveries (
    id              SERIAL PRIMARY KEY,
    order_id        INT NOT NULL REFERENCES orders(id) UNIQUE,
    deliverer_id    INT REFERENCES users(id),
    deliverer_type  TEXT NOT NULL DEFAULT 'rider' CHECK (deliverer_type IN ('rider','volunteer')),
    thermal_box_no  TEXT NOT NULL DEFAULT '',   -- 保温箱编号
    route_info      TEXT NOT NULL DEFAULT '',   -- 路线说明
    status          TEXT NOT NULL DEFAULT 'assigned' CHECK (status IN ('assigned','picked','delivered','failed')),
    pickup_time     TIMESTAMPTZ,
    delivered_time  TIMESTAMPTZ,
    sign_photo_url  TEXT NOT NULL DEFAULT '',   -- 送达/签收照片
    sign_type       TEXT NOT NULL DEFAULT '' CHECK (sign_type IN ('','elder','family','community','volunteer')),
    signed_by_name  TEXT NOT NULL DEFAULT '',
    knock_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
    timeout_minutes INT NOT NULL DEFAULT 60,    -- 超时阈值
    is_timeout      BOOLEAN NOT NULL DEFAULT FALSE,
    anomaly_note    TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 异常工单：未开门/餐盒未回收/家属临时改餐/厨房少做/骑手超时/补贴资格变更/饭菜不适合
CREATE TABLE IF NOT EXISTS anomalies (
    id           SERIAL PRIMARY KEY,
    order_id     INT REFERENCES orders(id),
    elder_id     INT REFERENCES elders(id),
    type         TEXT NOT NULL CHECK (type IN ('no_answer','box_not_returned','family_change','kitchen_shortage','rider_timeout','eligibility_changed','meal_unsuitable','other')),
    priority     TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('normal','high')),
    description  TEXT NOT NULL DEFAULT '',
    reported_by  INT REFERENCES users(id),
    status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','processing','resolved')),
    resolution   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ
);

-- 社区回访（由异常触发，与补贴核销联动）
CREATE TABLE IF NOT EXISTS follow_ups (
    id           SERIAL PRIMARY KEY,
    anomaly_id   INT NOT NULL REFERENCES anomalies(id),
    elder_id     INT NOT NULL REFERENCES elders(id),
    community_id INT REFERENCES users(id),
    type         TEXT NOT NULL DEFAULT 'phone' CHECK (type IN ('phone','visit')),
    elder_status TEXT NOT NULL DEFAULT '' CHECK (elder_status IN ('','fine','need_help','urgent')), -- 老人状态
    result       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 老人/家属反馈（饭菜是否适合）
CREATE TABLE IF NOT EXISTS feedbacks (
    id         SERIAL PRIMARY KEY,
    order_id   INT NOT NULL REFERENCES orders(id),
    elder_id   INT NOT NULL REFERENCES elders(id),
    from_user  INT REFERENCES users(id),
    rating     INT NOT NULL DEFAULT 5,
    suitable   BOOLEAN NOT NULL DEFAULT TRUE,
    content    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 餐盒回收台账
CREATE TABLE IF NOT EXISTS box_records (
    id             SERIAL PRIMARY KEY,
    order_id       INT NOT NULL REFERENCES orders(id) UNIQUE,
    elder_id       INT NOT NULL REFERENCES elders(id),
    boxes_issued   INT NOT NULL DEFAULT 0,
    boxes_returned INT NOT NULL DEFAULT 0,
    return_method  TEXT NOT NULL DEFAULT 'next_delivery',
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','partial','returned')),
    returned_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 补贴资格变更记录（变更本身也作为异常事件进入同一餐单视野）
CREATE TABLE IF NOT EXISTS subsidy_changes (
    id             SERIAL PRIMARY KEY,
    elder_id       INT NOT NULL REFERENCES elders(id),
    old_level      TEXT NOT NULL,
    new_level      TEXT NOT NULL,
    old_amount     NUMERIC(10,2) NOT NULL,
    new_amount     NUMERIC(10,2) NOT NULL,
    reason         TEXT NOT NULL DEFAULT '',
    changed_by     INT REFERENCES users(id),
    affected_orders INT NOT NULL DEFAULT 0, -- 被重算补贴的在途餐单数
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 月度核销（财政档案）
CREATE TABLE IF NOT EXISTS reconciliations (
    id                        SERIAL PRIMARY KEY,
    month                     TEXT NOT NULL UNIQUE,  -- YYYY-MM
    status                    TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','confirmed','archived')),
    total_orders              INT NOT NULL DEFAULT 0,
    signed_orders             INT NOT NULL DEFAULT 0,
    cancelled_orders          INT NOT NULL DEFAULT 0,
    exception_orders          INT NOT NULL DEFAULT 0,
    subsidy_total             NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 实际发放补贴（仅实际签收）
    refund_total              NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 退餐金额
    payable_total             NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 老人自付合计
    kitchen_settlement        NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 厨房结算金额
    anomaly_count             INT NOT NULL DEFAULT 0,
    followup_done             INT NOT NULL DEFAULT 0,
    boxes_issued              INT NOT NULL DEFAULT 0,
    boxes_returned            INT NOT NULL DEFAULT 0,
    recycle_rate              NUMERIC(5,2) NOT NULL DEFAULT 0,   -- 餐盒回收率 %
    holiday_extra_total       NUMERIC(12,2) NOT NULL DEFAULT 0,
    note                      TEXT NOT NULL DEFAULT '',
    created_by                INT REFERENCES users(id),
    confirmed_at              TIMESTAMPTZ,
    archived_at               TIMESTAMPTZ,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 核销明细：每一单是否纳入补贴发放及原因
CREATE TABLE IF NOT EXISTS reconciliation_items (
    id                SERIAL PRIMARY KEY,
    reconciliation_id INT NOT NULL REFERENCES reconciliations(id) ON DELETE CASCADE,
    order_id          INT NOT NULL REFERENCES orders(id),
    order_no          TEXT NOT NULL,
    elder_name        TEXT NOT NULL,
    meal_date         DATE NOT NULL,
    order_status      TEXT NOT NULL,
    total_amount      NUMERIC(10,2) NOT NULL DEFAULT 0,
    subsidy_amount    NUMERIC(10,2) NOT NULL DEFAULT 0,
    included          BOOLEAN NOT NULL DEFAULT FALSE, -- 是否纳入补贴发放
    reason            TEXT NOT NULL DEFAULT '',
    UNIQUE (reconciliation_id, order_id)
);

-- 站内通知：家属端/社区端/厨房端状态一致
CREATE TABLE IF NOT EXISTS notifications (
    id         SERIAL PRIMARY KEY,
    user_id    INT REFERENCES users(id),   -- 指定用户；为空则按角色广播
    role       TEXT NOT NULL DEFAULT '',
    order_id   INT,
    title      TEXT NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    read       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 已有库的幂等补充（CREATE TABLE IF NOT EXISTS 不会更新旧表结构）
ALTER TABLE elders ADD COLUMN IF NOT EXISTS risk_level TEXT NOT NULL DEFAULT 'normal';
ALTER TABLE elders ADD COLUMN IF NOT EXISTS delivery_confirm_mode TEXT NOT NULL DEFAULT 'direct';
ALTER TABLE elders ADD COLUMN IF NOT EXISTS no_answer_count INT NOT NULL DEFAULT 0;
ALTER TABLE elders ADD COLUMN IF NOT EXISTS focus_until DATE;
ALTER TABLE anomalies ADD COLUMN IF NOT EXISTS home_visit BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE elders ADD COLUMN IF NOT EXISTS elder_type TEXT NOT NULL DEFAULT 'normal';       -- 老人类型：normal/difficult（困难）
ALTER TABLE elders ADD COLUMN IF NOT EXISTS box_policy TEXT NOT NULL DEFAULT 'normal';       -- 餐盒策略：normal/disposable(一次性)/paused(暂停发放)
ALTER TABLE elders ADD COLUMN IF NOT EXISTS deposit_status TEXT NOT NULL DEFAULT 'none';     -- 押金状态：none/pending/paid/waive_pending/waived/refunded
ALTER TABLE box_records ADD COLUMN IF NOT EXISTS remind_count INT NOT NULL DEFAULT 0;        -- 提醒次数
ALTER TABLE box_records ADD COLUMN IF NOT EXISTS family_feedback TEXT NOT NULL DEFAULT '';   -- 家属反馈

-- 餐盒押金台账：收取/缴纳/免押审批/退还，留存社区负责人与回收志愿者防止责任空转
CREATE TABLE IF NOT EXISTS box_deposits (
    id                 SERIAL PRIMARY KEY,
    elder_id           INT NOT NULL REFERENCES elders(id),
    amount             NUMERIC(10,2) NOT NULL,
    status             TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','paid','waive_pending','waived','refunded')),
    unreturned_snapshot INT NOT NULL DEFAULT 0,   -- 触发时未回收数量
    family_feedback    TEXT NOT NULL DEFAULT '',  -- 家属反馈
    created_by         INT REFERENCES users(id),  -- 发起收取的社区人员
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at            TIMESTAMPTZ,
    waive_applicant_id INT REFERENCES users(id),  -- 免押申请：社区负责人
    waive_reason       TEXT NOT NULL DEFAULT '',
    waive_approver_id  INT REFERENCES users(id),  -- 免押审批人
    waived_at          TIMESTAMPTZ,
    volunteer_id       INT REFERENCES users(id),  -- 回收志愿者
    volunteer_name     TEXT NOT NULL DEFAULT '',
    refund_at          TIMESTAMPTZ
);

-- 餐盒库存：发放出账、回收入账（含志愿回收）
CREATE TABLE IF NOT EXISTS box_inventory (
    id         SERIAL PRIMARY KEY,
    location   TEXT NOT NULL UNIQUE,
    stock      INT NOT NULL DEFAULT 50,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 未开门联系尝试记录：敲门、电话、邻里询问、家属联系
CREATE TABLE IF NOT EXISTS contact_attempts (
    id           SERIAL PRIMARY KEY,
    delivery_id  INT NOT NULL REFERENCES deliveries(id),
    order_id     INT NOT NULL REFERENCES orders(id),
    elder_id     INT NOT NULL REFERENCES elders(id),
    knock_done   BOOLEAN NOT NULL DEFAULT FALSE,  -- 敲门
    phone_done   BOOLEAN NOT NULL DEFAULT FALSE,  -- 电话联系
    neighbor_done BOOLEAN NOT NULL DEFAULT FALSE, -- 邻里询问
    family_done  BOOLEAN NOT NULL DEFAULT FALSE,  -- 家属联系
    note         TEXT NOT NULL DEFAULT '',
    reported_by  INT REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 状态流转时间线（同一餐单各方看到一致状态）
CREATE TABLE IF NOT EXISTS order_events (
    id         SERIAL PRIMARY KEY,
    order_id   INT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    actor_id   INT REFERENCES users(id),
    actor_name TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    detail     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orders_meal_date ON orders(meal_date);
CREATE INDEX IF NOT EXISTS idx_orders_elder ON orders(elder_id);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_anomalies_status ON anomalies(status);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, read);
CREATE INDEX IF NOT EXISTS idx_notifications_role ON notifications(role, read);
CREATE INDEX IF NOT EXISTS idx_box_records_status ON box_records(status);
