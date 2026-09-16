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

-- ====================================================================
-- 老人状态变更（住院/转院/搬离/去世）与按生效日期四段清算
-- ====================================================================

-- 老人每月补贴可享次数（0 表示不限次）；服务状态（在服/住院暂停/搬离/去世）
ALTER TABLE elders ADD COLUMN IF NOT EXISTS monthly_quota INT NOT NULL DEFAULT 0;
ALTER TABLE elders ADD COLUMN IF NOT EXISTS service_status TEXT NOT NULL DEFAULT 'active'
    CHECK (service_status IN ('active','paused_hospital','moved_out','deceased'));
-- 菜品食材成本（元/份），用于已备餐/已出餐未送达的食材损耗核算
ALTER TABLE dishes ADD COLUMN IF NOT EXISTS unit_cost NUMERIC(10,2) NOT NULL DEFAULT 0;

-- 餐单状态补充：paused 住院/转院暂停（暂停期间不得核销补贴）
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN
    ('pending','confirmed','preparing','ready','delivering','signed','completed','exception',
     'cancelled','refunded','settled','paused'));

-- 老人状态变更主表：登记 生效日期/经办人/家属确认，驱动四段拆分与各方清算
CREATE TABLE IF NOT EXISTS elder_status_changes (
    id                       SERIAL PRIMARY KEY,
    elder_id                 INT NOT NULL REFERENCES elders(id),
    change_type              TEXT NOT NULL CHECK (change_type IN ('hospitalization','transfer','move_out','death','resume')),
    status                   TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','resumed','closed')),
    effective_date           DATE NOT NULL,
    -- 住院/转院登记信息
    hospital                 TEXT NOT NULL DEFAULT '',          -- 住院医院
    expected_discharge_date  DATE,                               -- 预计出院日
    family_contact_name      TEXT NOT NULL DEFAULT '',          -- 家属联系人
    family_contact_phone     TEXT NOT NULL DEFAULT '',
    subsidy_retained         BOOLEAN NOT NULL DEFAULT TRUE,     -- 补贴资格是否保留
    transfer_hospital        TEXT NOT NULL DEFAULT '',          -- 转入医院（转院）
    -- 搬离信息
    new_address              TEXT NOT NULL DEFAULT '',
    in_coverage              BOOLEAN NOT NULL DEFAULT TRUE,     -- 新地址是否在覆盖范围
    boxes_to_recover         INT NOT NULL DEFAULT 0,            -- 应回收餐盒/保温箱
    boxes_recovered          INT NOT NULL DEFAULT 0,
    -- 去世
    death_date               DATE,
    -- 出院恢复时重新核验四要素
    reverify_address         TEXT NOT NULL DEFAULT '',          -- 重新核验送餐地址
    reverify_dietary         TEXT NOT NULL DEFAULT '',          -- 饮食禁忌
    reverify_subsidy_level   TEXT NOT NULL DEFAULT '',          -- 补贴资格
    reverify_subsidy_amount  NUMERIC(10,2) NOT NULL DEFAULT 0,
    reverify_emergency_name  TEXT NOT NULL DEFAULT '',          -- 紧急联系人
    reverify_emergency_phone TEXT NOT NULL DEFAULT '',
    remaining_quota          INT NOT NULL DEFAULT 0,            -- 重算后当月剩余可享次数
    -- 四段清算汇总
    seg_signed               INT NOT NULL DEFAULT 0,
    seg_in_transit           INT NOT NULL DEFAULT 0,
    seg_prepared             INT NOT NULL DEFAULT 0,
    seg_unprepared           INT NOT NULL DEFAULT 0,
    signed_subsidy_total     NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 已签收：保留，正常财政结算
    transit_subsidy_total    NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 在途：暂停挂起，暂不核销
    prepared_refund_total    NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 已备餐未出餐：退餐金额
    prepared_cost_total      NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 已备餐批次食材成本
    unprepared_cancel_subsidy NUMERIC(12,2) NOT NULL DEFAULT 0, -- 未备餐：取消释放的补贴额度
    kitchen_loss_total       NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 已出餐未送达食材损耗（厨房责任）
    transferred_qty          INT NOT NULL DEFAULT 0,            -- 已出餐可转配份数
    operator_id              INT REFERENCES users(id),          -- 经办人
    family_confirmed_by      TEXT NOT NULL DEFAULT '',          -- 家属确认人
    family_confirmed_at      TIMESTAMPTZ,
    note                     TEXT NOT NULL DEFAULT '',
    parent_change_id         INT REFERENCES elder_status_changes(id), -- 出院恢复单关联的暂停单
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    resumed_at               TIMESTAMPTZ
);

-- 四段清算明细：每一单归入哪一段、批次/数量/成本/转配与处理方式
CREATE TABLE IF NOT EXISTS status_change_items (
    id                   SERIAL PRIMARY KEY,
    change_id            INT NOT NULL REFERENCES elder_status_changes(id) ON DELETE CASCADE,
    order_id             INT NOT NULL REFERENCES orders(id),
    segment              TEXT NOT NULL CHECK (segment IN ('signed','in_transit','prepared_undelivered','unprepared')),
    order_no             TEXT NOT NULL,
    meal_date            DATE NOT NULL,
    order_status_snapshot TEXT NOT NULL,
    batch_id             INT,
    batch_no             TEXT NOT NULL DEFAULT '',
    picked               BOOLEAN NOT NULL DEFAULT FALSE,        -- 骑手是否已取餐
    qty                  INT NOT NULL DEFAULT 1,                -- 本单份数
    total_amount         NUMERIC(10,2) NOT NULL DEFAULT 0,
    subsidy_amount       NUMERIC(10,2) NOT NULL DEFAULT 0,
    payable_amount       NUMERIC(10,2) NOT NULL DEFAULT 0,
    material_cost        NUMERIC(10,2) NOT NULL DEFAULT 0,      -- 食材成本
    handling             TEXT NOT NULL DEFAULT '',              -- 处理方式说明
    transferred          BOOLEAN NOT NULL DEFAULT FALSE,        -- 是否转配给其他老人
    kitchen_responsible  BOOLEAN NOT NULL DEFAULT FALSE,        -- 是否记厨房责任（损耗）
    note                 TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_status_changes_elder ON elder_status_changes(elder_id);
CREATE INDEX IF NOT EXISTS idx_status_changes_type ON elder_status_changes(change_type, status);
CREATE INDEX IF NOT EXISTS idx_status_change_items_change ON status_change_items(change_id);
CREATE INDEX IF NOT EXISTS idx_status_change_items_order ON status_change_items(order_id);

-- 在途餐单补贴冻结：住院/转院暂停或终止时，骑手已取餐未签收的餐单暂不核销，待异常办结（签收/退餐）后处理
ALTER TABLE orders ADD COLUMN IF NOT EXISTS subsidy_frozen BOOLEAN NOT NULL DEFAULT FALSE;

-- ====================================================================
-- 志愿者帮送与家属代订：签收效力、责任划分、本人回访、核销依据
-- ====================================================================

-- 志愿者资质（挂在 users 上，role='volunteer'）
ALTER TABLE users ADD COLUMN IF NOT EXISTS vol_org TEXT NOT NULL DEFAULT '';          -- 所属社区或组织
ALTER TABLE users ADD COLUMN IF NOT EXISTS vol_id_verified BOOLEAN NOT NULL DEFAULT FALSE; -- 身份是否核验
ALTER TABLE users ADD COLUMN IF NOT EXISTS vol_trained BOOLEAN NOT NULL DEFAULT FALSE;     -- 是否经过助餐培训
ALTER TABLE users ADD COLUMN IF NOT EXISTS vol_training_date DATE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS vol_eligible BOOLEAN NOT NULL DEFAULT TRUE;    -- 帮送资格（可暂停）
ALTER TABLE users ADD COLUMN IF NOT EXISTS vol_suspend_reason TEXT NOT NULL DEFAULT '';

-- 志愿者当日健康打卡（发车前核验）
CREATE TABLE IF NOT EXISTS volunteer_health (
    id            SERIAL PRIMARY KEY,
    volunteer_id  INT NOT NULL REFERENCES users(id),
    check_date    DATE NOT NULL,
    health_status TEXT NOT NULL DEFAULT 'healthy' CHECK (health_status IN ('healthy','unwell')),
    temperature   NUMERIC(4,1) NOT NULL DEFAULT 36.5,
    note          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (volunteer_id, check_date)
);

-- 家属代订授权（代订人≠实际用餐人）
ALTER TABLE orders ADD COLUMN IF NOT EXISTS proxy_relation TEXT NOT NULL DEFAULT '';       -- 代订人与老人关系
ALTER TABLE orders ADD COLUMN IF NOT EXISTS proxy_auth_method TEXT NOT NULL DEFAULT '';    -- 授权方式
ALTER TABLE orders ADD COLUMN IF NOT EXISTS proxy_contact_phone TEXT NOT NULL DEFAULT '';  -- 代订人联系电话
ALTER TABLE orders ADD COLUMN IF NOT EXISTS proxy_name TEXT NOT NULL DEFAULT '';           -- 代订人姓名快照
-- 签收效力与依据：valid 有效 / pending 待社区核实 / invalid 经核实无效
ALTER TABLE orders ADD COLUMN IF NOT EXISTS sign_effectiveness TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS sign_basis TEXT NOT NULL DEFAULT '';           -- self/family/neighbor/photo/community

-- 配送记录补充志愿者帮送核验与签收留痕
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS vol_org TEXT NOT NULL DEFAULT '';          -- 所属组织快照
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS relation_to_elder TEXT NOT NULL DEFAULT '';-- 志愿者与老人关系
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS informed_consent BOOLEAN NOT NULL DEFAULT FALSE; -- 老人/家属是否知情同意
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS consent_by TEXT NOT NULL DEFAULT '';       -- 同意人（老人本人/家属姓名）
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS dispatch_checked BOOLEAN NOT NULL DEFAULT FALSE; -- 发车前五项核验通过
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS eta TIMESTAMPTZ;                           -- 预计送达时间
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS witness_name TEXT NOT NULL DEFAULT '';     -- 邻里见证人姓名
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS witness_phone TEXT NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS effectiveness TEXT NOT NULL DEFAULT '';    -- valid/pending/invalid
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS effectiveness_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS community_verified_by INT REFERENCES users(id);
ALTER TABLE deliveries ADD COLUMN IF NOT EXISTS community_verified_at TIMESTAMPTZ;

-- 放宽签收方式：本人/家属代签/社区/志愿者/邻里见证/拍照留证
ALTER TABLE deliveries DROP CONSTRAINT IF EXISTS deliveries_sign_type_check;
ALTER TABLE deliveries ADD CONSTRAINT deliveries_sign_type_check CHECK
    (sign_type IN ('','elder','family','community','volunteer','neighbor','photo'));

-- 餐单状态补充：签收待社区核实（邻里见证/拍照留证，未核实前不核销）
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN
    ('pending','confirmed','preparing','ready','delivering','signed','completed','exception',
     'cancelled','refunded','settled','paused','verify_pending'));

-- 异常工单补充：帮送责任划分与社区核实结论
ALTER TABLE anomalies ADD COLUMN IF NOT EXISTS responsible_party TEXT NOT NULL DEFAULT ''
    CHECK (responsible_party IN ('','rider','volunteer','kitchen','community','elder','none'));
ALTER TABLE anomalies ADD COLUMN IF NOT EXISTS food_safety BOOLEAN NOT NULL DEFAULT FALSE;  -- 涉及食品安全
ALTER TABLE anomalies ADD COLUMN IF NOT EXISTS elder_unwell BOOLEAN NOT NULL DEFAULT FALSE;-- 老人身体不适
ALTER TABLE anomalies ADD COLUMN IF NOT EXISTS investigation TEXT NOT NULL DEFAULT '';     -- 社区核实记录
ALTER TABLE anomalies ADD COLUMN IF NOT EXISTS outcome TEXT NOT NULL DEFAULT '';            -- redeliver/refund/resign/none
-- 异常类型补充：志愿者帮送异常（洒漏/迟到/送错/否认收到）
ALTER TABLE anomalies DROP CONSTRAINT IF EXISTS anomalies_type_check;
ALTER TABLE anomalies ADD CONSTRAINT anomalies_type_check CHECK (type IN
    ('no_answer','box_not_returned','family_change','kitchen_shortage','rider_timeout',
     'eligibility_changed','meal_unsuitable','other','volunteer_delivery'));

-- 老人本人/同住人回访（家属代订不得代老人放弃权益；签收核实以本人陈述为准）
CREATE TABLE IF NOT EXISTS elder_confirmations (
    id                SERIAL PRIMARY KEY,
    order_id          INT NOT NULL REFERENCES orders(id),
    elder_id          INT NOT NULL REFERENCES elders(id),
    confirmer_role    TEXT NOT NULL DEFAULT 'elder' CHECK (confirmer_role IN ('elder','cohabitant')),
    confirmer_name    TEXT NOT NULL DEFAULT '',
    method            TEXT NOT NULL DEFAULT 'phone' CHECK (method IN ('phone','visit','onsite')),
    confirms_received BOOLEAN NOT NULL DEFAULT TRUE,   -- 确认实际收到/用餐
    taste_feedback    TEXT NOT NULL DEFAULT '',        -- 口味反馈
    body_discomfort   BOOLEAN NOT NULL DEFAULT FALSE,  -- 身体不适
    receipt_dispute   BOOLEAN NOT NULL DEFAULT FALSE,  -- 否认收到/签收异常
    note              TEXT NOT NULL DEFAULT '',
    community_id      INT REFERENCES users(id),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_elder_confirm_order ON elder_confirmations(order_id);

-- 核销明细补充真实签收依据
ALTER TABLE reconciliation_items ADD COLUMN IF NOT EXISTS sign_basis TEXT NOT NULL DEFAULT '';
ALTER TABLE reconciliation_items ADD COLUMN IF NOT EXISTS sign_effectiveness TEXT NOT NULL DEFAULT '';

-- 社区是否授权该老人可采用邻里见证/志愿者拍照留证签收（认知障碍等严格对象不适用）
ALTER TABLE elders ADD COLUMN IF NOT EXISTS proxy_sign_authorized BOOLEAN NOT NULL DEFAULT FALSE;

-- 异常责任经办人快照（补送会清空 deliveries.deliverer_id，用此列保留志愿者考核关联）
ALTER TABLE anomalies ADD COLUMN IF NOT EXISTS responsible_user_id INT REFERENCES users(id);
