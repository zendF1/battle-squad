# Equipment System Design

Tài liệu thiết kế hệ thống trang bị cho Battle Squad.

---

## MỤC LỤC

- [1. Tổng quan](#1-tổng-quan)
- [2. Slot trang bị](#2-slot-trang-bị)
- [3. Trang bị thường (Shop)](#3-trang-bị-thường-shop)
- [4. Trang bị chế tạo (Crafted)](#4-trang-bị-chế-tạo-crafted)
- [5. Set bonus (Crafted only)](#5-set-bonus-crafted-only)
- [6. Hệ thống nâng cấp trang bị](#6-hệ-thống-nâng-cấp-trang-bị)
- [7. Đá nâng cấp](#7-đá-nâng-cấp)
- [8. Ghép đá nâng cấp](#8-ghép-đá-nâng-cấp)
- [9. Hệ thống ngọc khảm](#9-hệ-thống-ngọc-khảm)
- [10. Ghép ngọc](#10-ghép-ngọc)
- [11. Lock / Unlock & Bán lại](#11-lock--unlock--bán-lại)
- [12. Tách trang bị](#12-tách-trang-bị)
- [13. Gợi ý chỉ số](#13-gợi-ý-chỉ-số)
- [14. Gợi ý nguyên liệu chế tạo](#14-gợi-ý-nguyên-liệu-chế-tạo)
- [15. Tác động lên combat](#15-tác-động-lên-combat)
- [16. Database schema](#16-database-schema)
- [17. API endpoints](#17-api-endpoints)
- [18. Admin config](#18-admin-config)

---

## 1. Tổng quan

### Hai dòng trang bị

| | Trang bị thường | Trang bị chế tạo |
|---|---|---|
| Nguồn | Mua trong shop bằng **coin** | Chế tạo từ nguyên liệu |
| Sức mạnh | Cơ bản | Mạnh hơn |
| Level yêu cầu | 10, 20, 30, 40 | 15, 35, 55, 75 |
| Cấp độ | Cấp 1 → 4 (theo level) | Bạc, Vàng, Titan, Kim cương |
| Ô ngọc | 1 ô | 2 ô |
| Set bonus | Không | Có (đủ 6 món) |
| Nâng cấp | Có (+0 → +16) | Có (+0 → +16) |

### Hệ thống phụ trợ

- **Đá nâng cấp** — 12 cấp độ, dùng để nâng cấp trang bị
- **Ghép đá** — 4 viên cùng cấp → 1 viên cấp cao hơn
- **Ngọc khảm** — 4 loại (HP, Damage, Defense, Critical), khảm vào ô ngọc
- **Ghép ngọc** — 4 viên cùng cấp → 1 viên cấp cao hơn
- **Tách trang bị** — phân rã, nhận lại 50% đá đã dùng

---

## 2. Slot trang bị

Mỗi nhân vật (character) có **6 slot** trang bị:

| Slot | ID | Mô tả |
|------|----|-------|
| Vũ khí | `weapon` | Tăng chủ yếu Damage |
| Áo | `armor` | Tăng chủ yếu Defense + HP |
| Nón | `helmet` | Tăng Defense + HP |
| Quần | `pants` | Tăng Defense + HP |
| Giày | `boots` | Tăng chủ yếu Move Energy + nhẹ Defense |
| Găng tay | `gloves` | Tăng chủ yếu Critical + nhẹ Damage |

### Quy tắc

- Mỗi slot chỉ mặc được 1 trang bị cùng loại slot
- Trang bị có yêu cầu **character level** tối thiểu
- Trang bị mua từ shop dành riêng cho từng nhân vật (character-specific)
- Trang bị chế tạo dùng chung cho mọi nhân vật

---

## 3. Trang bị thường (Shop)

### Phân cấp theo level

| Cấp | Level yêu cầu | Tiền tệ | Định vị |
|-----|---------------|---------|---------|
| Cấp 1 | 10 | Coin | Starter gear |
| Cấp 2 | 20 | Coin | Early mid-game |
| Cấp 3 | 30 | Coin | Late mid-game |
| Cấp 4 | 40 | Coin | Pre-crafted |

### Đặc điểm

- Mua trực tiếp trong shop, **theo từng nhân vật** (mỗi character có shop riêng)
- Thanh toán bằng **coin** (currency farm từ chơi game)
- Khi mua: trạng thái **unlock** (có thể bán lại)
- Khi mặc vào: trạng thái **lock** (không bán lại được)
- **1 ô ngọc** per item
- **Không có set bonus**

### Ví dụ shop Rookie (nhân vật Rookie)

| Item | Slot | Level | Giá (Coin) |
|------|------|-------|-----------|
| Rookie Sword Lv10 | weapon | 10 | 500 |
| Rookie Vest Lv10 | armor | 10 | 400 |
| Rookie Cap Lv10 | helmet | 10 | 300 |
| Rookie Pants Lv10 | pants | 10 | 350 |
| Rookie Sneakers Lv10 | boots | 10 | 300 |
| Rookie Gloves Lv10 | gloves | 10 | 250 |
| ... | ... | 20, 30, 40 | ... (giá tăng dần) |

---

## 4. Trang bị chế tạo (Crafted)

### Phân cấp

| Cấp | Tên | Level yêu cầu | Sức mạnh | Nguyên liệu |
|-----|-----|---------------|----------|-------------|
| 1 | Bạc (Silver) | 15 | 1.3× thường Lv10 | Dễ kiếm |
| 2 | Vàng (Gold) | 35 | 1.3× thường Lv30 | Trung bình |
| 3 | Titan (Titan) | 55 | 1.5× thường Lv40 | Khó kiếm |
| 4 | Kim cương (Diamond) | 75 | 2.0× thường Lv40 | Rất khó kiếm |

### Đặc điểm

- Chế tạo từ **nguyên liệu** (crafting materials)
- Nguyên liệu có 2 nguồn:
  - **Phó bản** (dungeon drop) — rơi ra 3/5 loại nguyên liệu cần thiết
  - **Shop gem** — 2/5 loại nguyên liệu bắt buộc mua bằng **gem** (premium currency)
- Dùng chung cho mọi nhân vật (không character-specific)
- Khi chế tạo xong: trạng thái **unlock**
- Khi mặc vào: trạng thái **lock**
- **2 ô ngọc** per item
- **Có set bonus** khi đủ 6 món cùng cấp

### Công thức chế tạo (1 item)

```
Nguyên liệu A (drop) × số lượng
Nguyên liệu B (drop) × số lượng
Nguyên liệu C (drop) × số lượng
Nguyên liệu D (gem shop) × số lượng
Nguyên liệu E (gem shop) × số lượng
───────────────────────────────────
→ 1 Trang bị chế tạo
```

Tỷ lệ: 60% nguyên liệu farm free, 40% bắt buộc mua gem → monetization.

---

## 5. Set bonus (Crafted only)

### Bộ trang bị = 6 món cùng cấp chế tạo

Khi mặc đủ 6 món cùng tier (Bạc/Vàng/Titan/Kim cương), nhận bonus bổ sung:

| Số món | Bonus |
|--------|-------|
| 2 món | Bonus nhỏ (VD: +3% HP) |
| 4 món | Bonus trung bình (VD: +5% Damage) |
| 6 món (full) | Bonus lớn + hiệu ứng đặc biệt |

### Set bonus theo tier

| Set | 2 món | 4 món | 6 món (full set) |
|-----|-------|-------|------------------|
| Bạc | +3% HP | +3% DEF | +5% HP, +5% DEF |
| Vàng | +5% HP | +5% DMG | +8% HP, +8% DMG, +5% DEF |
| Titan | +8% HP | +8% DMG | +12% HP, +12% DMG, +8% DEF, +5% Crit |
| Kim cương | +10% HP | +10% DMG | +15% HP, +15% DMG, +12% DEF, +10% Crit |

### Quy tắc

- Chỉ trang bị **chế tạo** mới có set bonus, trang bị thường không có
- Bonus tính theo % cộng thêm trên tổng base stat sau trang bị
- Không stack 2 set khác tier (VD: 3 Bạc + 3 Vàng = không có bonus nào)
- Set bonus hiển thị real-time khi player thay đổi trang bị

---

## 6. Hệ thống nâng cấp trang bị

### Tổng quan

- Trang bị nâng cấp từ **+0 đến +16**
- Dùng **đá nâng cấp** (upgrade stone) để nâng
- Mỗi cấp nâng cấp yêu cầu đá ở level tối thiểu nhất định
- Tỷ lệ thành công giảm dần mỗi 2 cấp
- Tỷ lệ thành công **config được** trong Admin Dashboard → Game Config

### Bảng nâng cấp

| Nâng cấp | Đá tối thiểu | Tỷ lệ gợi ý | Thất bại |
|----------|-------------|-------------|----------|
| +0 → +1 | Đá cấp 1 | 100% | Giữ +0 |
| +1 → +2 | Đá cấp 1 | 95% | Giữ +1 |
| +2 → +3 | Đá cấp 2 | 90% | Giữ +2 |
| +3 → +4 | Đá cấp 2 | 85% | Giữ +3 |
| +4 → +5 | Đá cấp 3 | 75% | Giữ +4 |
| +5 → +6 | Đá cấp 3 | 65% | Giữ +5 |
| +6 → +7 | Đá cấp 5 | 50% | **Quay về +6** |
| +7 → +8 | Đá cấp 5 | 40% | **Quay về +6** |
| +8 → +9 | Đá cấp 7 | 30% | **Quay về +6** |
| +9 → +10 | Đá cấp 7 | 25% | **Quay về +6** |
| +10 → +11 | Đá cấp 9 | 20% | **Quay về +10** |
| +11 → +12 | Đá cấp 9 | 15% | **Quay về +10** |
| +12 → +13 | Đá cấp 10 | 10% | **Quay về +10** |
| +13 → +14 | Đá cấp 10 | 8% | **Quay về +10** |
| +14 → +15 | Đá cấp 11 | 5% | **Quay về +14** |
| +15 → +16 | Đá cấp 12 | 3% | **Quay về +14** |

> Tỷ lệ là gợi ý, **cấu hình được từ Admin Dashboard**.

### Milestone bonus

Khi đạt các mốc nâng cấp, chỉ số trang bị được cộng thêm % bonus:

| Mốc | Bonus chỉ số | Hiệu ứng |
|------|-------------|-----------|
| +6 | +10% tổng stat | Tên item hiển thị màu xanh lá |
| +10 | +20% tổng stat | Tên item hiển thị màu xanh dương |
| +14 | +40% tổng stat | Tên item hiển thị màu tím |
| +16 | +100% tổng stat | Tên item hiển thị màu đỏ/vàng (legendary) |

### Quy tắc thất bại (Safezone)

```
Cấp +1 → +6:  Thất bại → GIỮA NGUYÊN cấp hiện tại (không tụt)
Cấp +6 → +10: Thất bại → quay về +6 (safezone 1)
Cấp +10 → +14: Thất bại → quay về +10 (safezone 2)
Cấp +14 → +16: Thất bại → quay về +14 (safezone 3)
```

### Tính chỉ số nâng cấp

```
final_stat = base_stat × (1 + upgrade_bonus + milestone_bonus)

Trong đó:
- base_stat: chỉ số gốc của trang bị
- upgrade_bonus: +2% per upgrade level = level × 0.02
  VD: +8 = 0.16 (16%)
- milestone_bonus: 0.10 / 0.20 / 0.40 / 1.00 tùy mốc đạt được
  VD: +10 nhận cả bonus +6 và +10 = 0.10 + 0.20 = 0.30

Ví dụ: Vũ khí base DMG 50, nâng lên +10
  upgrade_bonus = 10 × 0.02 = 0.20
  milestone_bonus = 0.10 (+6) + 0.20 (+10) = 0.30
  final = 50 × (1 + 0.20 + 0.30) = 50 × 1.50 = 75 DMG
```

---

## 7. Đá nâng cấp (Upgrade Stone)

### 12 cấp độ đá

| Cấp đá | % thêm khi nâng cấp | Nguồn |
|--------|---------------------|-------|
| Đá cấp 1 | Base rate | Shop (coin) |
| Đá cấp 2 | +2% | Shop (coin) |
| Đá cấp 3 | +4% | Shop (coin) |
| Đá cấp 4 | +6% | Shop (coin) |
| Đá cấp 5 | +8% | Shop (coin) |
| Đá cấp 6 | +10% | Shop (coin) |
| Đá cấp 7 | +13% | Shop (**gem**) |
| Đá cấp 8 | +16% | Shop (**gem**) |
| Đá cấp 9 | +20% | Shop (**gem**) |
| Đá cấp 10 | +25% | Shop (**gem**) |
| Đá cấp 11 | Không bán | Chỉ ghép (4 × đá cấp 10) |
| Đá cấp 12 | Không bán | Chỉ ghép (4 × đá cấp 11) |

### Nguồn thu thập

| Nguồn | Cấp đá rơi ra | Điều kiện |
|-------|--------------|-----------|
| Shop (coin) | 1 → 6 | Mua trực tiếp |
| Shop (gem) | 7 → 10 | Mua trực tiếp |
| Thắng match PvP | 1 → 3 | Random sau trận thắng |
| Thắng match Ranked | 2 → 4 | Tùy tier ranking |
| Phó bản (tương lai) | 3 → 6 | Tùy độ khó phó bản |
| Ghép đá | 2 → 12 | 4 viên cùng cấp → 1 viên cấp trên |

### Cách dùng đá khi nâng cấp

```
Tỷ lệ thực tế = Tỷ lệ base (config) + % bonus từ cấp đá

Ví dụ: Nâng +6 → +7, tỷ lệ base 50%
  Dùng đá cấp 5 (tối thiểu): 50% + 8% = 58%
  Dùng đá cấp 8 (cao hơn): 50% + 16% = 66%
  Dùng đá cấp 10: 50% + 25% = 75%

→ Đá cấp cao hơn yêu cầu tối thiểu sẽ tăng tỷ lệ thành công.
```

---

## 8. Ghép đá nâng cấp (Stone Merge)

### Công thức

```
4 × Đá cấp N → 1 × Đá cấp (N+1)
```

### Quy tắc

- Chỉ ghép đá **cùng cấp độ**
- Cần đúng **4 viên** để ghép
- Tỷ lệ thành công: mỗi viên đóng góp **25%**, tối đa **50%**

### Cơ chế tỷ lệ

| Số viên bỏ vào | Tỷ lệ | Cho phép ghép? |
|----------------|--------|---------------|
| 1 viên | 25% | Không (dưới 50%) |
| 2 viên | 50% | **Có** (đạt 50% — tối đa) |
| 3 viên | 50% | Có (cap tại 50%) |
| 4 viên | 50% | Có (cap tại 50%) |

> Cần tối thiểu **2 viên** (50%) để ghép. Bỏ thêm viên không tăng tỷ lệ (cap 50%).
> 4 viên bỏ vào = consume cả 4, nhưng tỷ lệ vẫn 50%.

### Kết quả

- **Thành công:** nhận 1 viên cấp (N+1), mất toàn bộ viên bỏ vào
- **Thất bại:** mất toàn bộ viên bỏ vào, không nhận gì

### Giới hạn

- Đá cấp 12 là cấp cao nhất, không ghép tiếp được
- Ghép đá cấp 10 → cấp 11: cách duy nhất lấy đá cấp 11 (không bán trong shop)
- Ghép đá cấp 11 → cấp 12: cách duy nhất lấy đá cấp 12

---

## 9. Hệ thống ngọc khảm (Gem Socket)

### 4 loại ngọc

| Loại ngọc | Stat tăng | Màu |
|-----------|----------|-----|
| Ngọc HP | +HP flat | Xanh lá |
| Ngọc Damage | +Damage flat | Đỏ |
| Ngọc Defense | +Defense flat | Xanh dương |
| Ngọc Critical | +Critical % | Vàng |

### Cấp độ ngọc

| Cấp | HP | Damage | Defense | Critical |
|-----|-----|--------|---------|----------|
| 1 | +20 | +3 | +2 | +1% |
| 2 | +40 | +6 | +4 | +2% |
| 3 | +70 | +10 | +7 | +3% |
| 4 | +110 | +15 | +11 | +5% |
| 5 | +160 | +22 | +16 | +7% |
| 6 | +220 | +30 | +22 | +9% |
| 7 | +300 | +40 | +30 | +12% |
| 8 | +400 | +52 | +40 | +15% |
| 9 | +520 | +67 | +52 | +18% |
| 10 | +660 | +85 | +66 | +22% |

> Chỉ số gợi ý, cấu hình được từ Admin Dashboard.

### Ô ngọc

| Loại trang bị | Số ô ngọc |
|--------------|----------|
| Trang bị thường | 1 ô |
| Trang bị chế tạo | 2 ô |

### Quy tắc khảm

- Mỗi ô chỉ khảm được **1 viên** ngọc
- Có thể khảm **bất kỳ loại ngọc nào** vào bất kỳ slot nào
- Trang bị chế tạo 2 ô: có thể khảm 2 viên cùng loại hoặc khác loại
- Tháo ngọc: **miễn phí**, ngọc trả về inventory nguyên vẹn

### Nguồn ngọc

| Nguồn | Cấp ngọc |
|-------|---------|
| Shop (coin) | Cấp 1 → 3 |
| Shop (gem) | Cấp 4 → 6 |
| Thắng match Ranked | Cấp 1 → 2 (random, tỷ lệ thấp) |
| Ghép ngọc | Cấp 2 → 10 |
| Phó bản (tương lai) | Cấp 2 → 5 |

---

## 10. Ghép ngọc (Gem Merge)

Cơ chế giống hệt ghép đá nâng cấp:

### Công thức

```
4 × Ngọc [loại X] cấp N → 1 × Ngọc [loại X] cấp (N+1)
```

### Quy tắc

- Chỉ ghép ngọc **cùng loại VÀ cùng cấp** (VD: 4 × Ngọc HP cấp 3 → 1 × Ngọc HP cấp 4)
- Không ghép khác loại (HP + DMG = không được)
- Tỷ lệ: mỗi viên 25%, cap 50%, tối thiểu 2 viên
- Thất bại: mất toàn bộ ngọc bỏ vào
- Cấp 10 là cấp cao nhất

---

## 11. Lock / Unlock & Bán lại

### Trạng thái trang bị

| Trạng thái | Khi nào | Bán lại? |
|------------|---------|---------|
| **Unlock** | Vừa mua từ shop / vừa chế tạo xong | Có thể bán |
| **Lock** | Đã mặc vào nhân vật (equip) | Không bán được |

### Quy tắc

- Mua từ shop → **unlock** → có thể bán lại ngay nếu chưa mặc
- Chế tạo xong → **unlock** → có thể bán lại ngay nếu chưa mặc
- Equip vào nhân vật → **lock vĩnh viễn** → không thể bán lại
- Tháo ra (unequip) → vẫn **lock** → không đổi lại unlock

### Bán lại (tương lai)

- Hệ thống chợ giao dịch (marketplace) giữa các player: **chưa hỗ trợ**
- Khi có marketplace: chỉ item **unlock** mới list bán được
- Bán cho NPC/system: có thể hỗ trợ, nhận lại % coin

---

## 12. Tách trang bị (Dismantle)

### Mục đích

Phân rã trang bị đã nâng cấp để thu hồi một phần đá nâng cấp.

### Công thức thu hồi

```
Nhận lại 50% tổng đá đã dùng ở MỐC HIỆN TẠI.
Đá nâng cấp của các mốc trước KHÔNG được hoàn.
```

### Ví dụ chi tiết

**Trang bị +8:**

Giả sử để nâng từ +6 → +8 cần tổng 5 viên đá cấp 7 (bao gồm cả lần fail):

```
Mốc hiện tại: +6 → +8 (thuộc safezone +6 → +10)
Đá đã dùng ở mốc này: 5 viên đá cấp 7
Thu hồi 50%: 2.5 viên → làm tròn xuống

Nhận lại:
  2 × Đá cấp 7
  (0.5 viên còn lại → quy đổi xuống 1 × Đá cấp 6)

Tổng nhận: 2 × Đá cấp 7 + 1 × Đá cấp 6 (nếu có phần lẻ)
```

**Đá dùng cho +0 → +6: KHÔNG hoàn lại.**

### Quy tắc

- Tách trang bị sẽ **xóa trang bị** khỏi inventory
- Ngọc đang khảm sẽ **trả về inventory** trước khi tách
- Trang bị chưa nâng cấp (+0): tách không nhận được gì (chỉ xóa)
- Hệ thống ghi log lịch sử đá đã dùng per item để tính chính xác

---

## 13. Gợi ý chỉ số (Stat Suggestion)

### Base stat trang bị thường

**Vũ khí (weapon) — Damage chủ lực:**

| Cấp | Level | DMG | HP | DEF | Crit |
|-----|-------|-----|-----|-----|------|
| 1 | 10 | 8 | 0 | 0 | 0% |
| 2 | 20 | 15 | 0 | 0 | 1% |
| 3 | 30 | 25 | 0 | 0 | 2% |
| 4 | 40 | 38 | 0 | 0 | 3% |

**Áo (armor) — Defense + HP chủ lực:**

| Cấp | Level | DMG | HP | DEF | Crit |
|-----|-------|-----|-----|-----|------|
| 1 | 10 | 0 | 30 | 3 | 0% |
| 2 | 20 | 0 | 60 | 6 | 0% |
| 3 | 30 | 0 | 100 | 10 | 0% |
| 4 | 40 | 0 | 150 | 15 | 0% |

**Nón (helmet) — HP + Defense:**

| Cấp | Level | DMG | HP | DEF | Crit |
|-----|-------|-----|-----|-----|------|
| 1 | 10 | 0 | 20 | 2 | 0% |
| 2 | 20 | 0 | 40 | 4 | 0% |
| 3 | 30 | 0 | 70 | 7 | 0% |
| 4 | 40 | 0 | 110 | 11 | 0% |

**Quần (pants) — Defense + HP:**

| Cấp | Level | DMG | HP | DEF | Crit |
|-----|-------|-----|-----|-----|------|
| 1 | 10 | 0 | 20 | 2 | 0% |
| 2 | 20 | 0 | 45 | 5 | 0% |
| 3 | 30 | 0 | 75 | 8 | 0% |
| 4 | 40 | 0 | 120 | 12 | 0% |

**Giày (boots) — Move Energy + nhẹ Defense:**

| Cấp | Level | DMG | HP | DEF | Move Energy |
|-----|-------|-----|-----|-----|-------------|
| 1 | 10 | 0 | 10 | 1 | +5 |
| 2 | 20 | 0 | 20 | 2 | +10 |
| 3 | 30 | 0 | 35 | 4 | +15 |
| 4 | 40 | 0 | 55 | 6 | +20 |

**Găng tay (gloves) — Critical + nhẹ Damage:**

| Cấp | Level | DMG | HP | DEF | Crit |
|-----|-------|-----|-----|-----|------|
| 1 | 10 | 2 | 0 | 0 | 2% |
| 2 | 20 | 4 | 0 | 0 | 4% |
| 3 | 30 | 7 | 0 | 0 | 6% |
| 4 | 40 | 12 | 0 | 0 | 8% |

### Base stat trang bị chế tạo

Mạnh hơn trang bị thường cùng level range:

| Tier | So với thường | Vũ khí DMG | Áo HP | Áo DEF |
|------|-------------|-----------|-------|--------|
| Bạc (Lv15) | 1.3× Lv10 | 10 | 40 | 4 |
| Vàng (Lv35) | 1.3× Lv30 | 33 | 130 | 13 |
| Titan (Lv55) | 1.5× Lv40 | 57 | 225 | 23 |
| Kim cương (Lv75) | 2.0× Lv40 | 76 | 300 | 30 |

> Tất cả chỉ số là gợi ý, cấu hình từ Admin Dashboard.

---

## 14. Gợi ý nguyên liệu chế tạo

### Cấu trúc nguyên liệu mỗi tier

Mỗi tier chế tạo cần **5 loại nguyên liệu**, trong đó:
- **3 loại** drop từ phó bản / gameplay (free)
- **2 loại** chỉ mua được bằng **gem** (premium)

### Bộ nguyên liệu Bạc (Silver)

| # | Nguyên liệu | Nguồn | Số lượng/item |
|---|-------------|-------|--------------|
| 1 | Quặng bạc (Silver Ore) | Phó bản / Match reward | 5 |
| 2 | Sợi dệt (Woven Thread) | Phó bản / Match reward | 3 |
| 3 | Da thú (Beast Hide) | Phó bản / Match reward | 3 |
| 4 | Bột kết tinh (Crystal Powder) | **Gem shop** | 2 |
| 5 | Bản thiết kế bạc (Silver Blueprint) | **Gem shop** | 1 |

### Bộ nguyên liệu Vàng (Gold)

| # | Nguyên liệu | Nguồn | Số lượng/item |
|---|-------------|-------|--------------|
| 1 | Quặng vàng (Gold Ore) | Phó bản | 8 |
| 2 | Lụa ma thuật (Enchanted Silk) | Phó bản | 5 |
| 3 | Vảy rồng nhỏ (Small Dragon Scale) | Phó bản | 4 |
| 4 | Tinh chất ma thuật (Magic Essence) | **Gem shop** | 3 |
| 5 | Bản thiết kế vàng (Gold Blueprint) | **Gem shop** | 1 |

### Bộ nguyên liệu Titan

| # | Nguyên liệu | Nguồn | Số lượng/item |
|---|-------------|-------|--------------|
| 1 | Quặng titan (Titan Ore) | Phó bản | 12 |
| 2 | Sợi titan (Titan Fiber) | Phó bản | 8 |
| 3 | Lõi năng lượng (Energy Core) | Phó bản | 6 |
| 4 | Dung dịch titan (Titan Solvent) | **Gem shop** | 4 |
| 5 | Bản thiết kế titan (Titan Blueprint) | **Gem shop** | 1 |

### Bộ nguyên liệu Kim cương (Diamond)

| # | Nguyên liệu | Nguồn | Số lượng/item |
|---|-------------|-------|--------------|
| 1 | Kim cương thô (Raw Diamond) | Phó bản | 15 |
| 2 | Sợi ánh sáng (Light Weave) | Phó bản | 10 |
| 3 | Tinh hồn boss (Boss Soul Shard) | Phó bản (boss drop) | 8 |
| 4 | Nước mắt phượng hoàng (Phoenix Tear) | **Gem shop** | 5 |
| 5 | Bản thiết kế kim cương (Diamond Blueprint) | **Gem shop** | 1 |

---

## 15. Tác động lên combat

### Cách stat trang bị ảnh hưởng trận đấu

| Stat | Ảnh hưởng |
|------|----------|
| **Damage (DMG)** | Cộng vào base damage của vũ khí nhân vật. `finalDamage = weaponDamage + equipDMG` |
| **HP** | Cộng vào max HP. `finalHP = baseHP + equipHP` |
| **Defense (DEF)** | Giảm damage nhận vào. `damageTaken = incomingDamage × (100 / (100 + DEF))` |
| **Critical (Crit %)** | % cơ hội gây damage x1.5. Roll mỗi phát bắn. |
| **Move Energy** | Cộng vào energy mỗi lượt. `turnEnergy = 100 + equipMoveEnergy` |

### Công thức damage đầy đủ

```
1. Base damage = weaponConfig.damage + totalEquipDMG
2. Skill modifier (nếu dùng skill): damage *= skillConfig.damageMultiplier
3. Wind effect: đạn bay theo wind, ảnh hưởng landing point
4. Distance falloff: damage *= (1 - distance/explosionRadius)
5. Critical roll: if rand() < totalCritPercent → damage *= 1.5
6. Defense reduction: finalDamage = damage × (100 / (100 + targetDEF))
7. Clamp: finalDamage = max(1, round(finalDamage))
```

### Power budget (full gear ví dụ)

**Player Lv40, full trang bị thường cấp 4, +0, không ngọc:**

| Stat | Base (no equip) | + Full gear | Tổng |
|------|-----------------|------------|------|
| HP | 500 | +435 | 935 |
| DMG | 30 | +50 | 80 |
| DEF | 0 | +44 | 44 |
| Crit | 0% | +11% | 11% |
| Move Energy | 100 | +20 | 120 |

**Player Lv75, full Kim cương +10, 2 ngọc cấp 7 mỗi item, full set bonus:**

| Stat | Base | Gear | +10 bonus | Gems | Set bonus | Tổng |
|------|------|------|-----------|------|-----------|------|
| HP | 500 | +1600 | +800 | +3600 | +15% | ~7475 |
| DMG | 30 | +100 | +50 | +480 | +15% | ~758 |
| DEF | 0 | +150 | +75 | +360 | +12% | ~655 |
| Crit | 0% | +20% | +10% | +24% | +10% | 64% |

> Chỉ số minh họa. Cần balance test thực tế.

---

## 16. Database schema

### Bảng mới cần tạo

```sql
-- Trang bị đang sở hữu
CREATE TABLE IF NOT EXISTS player_equipment (
    equipment_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id       VARCHAR(50) NOT NULL REFERENCES player_profiles(player_id),
    item_id         VARCHAR(100) NOT NULL,       -- FK tới config_equipment_items
    slot            VARCHAR(20) NOT NULL,         -- weapon/armor/helmet/pants/boots/gloves
    category        VARCHAR(20) NOT NULL,         -- 'normal' hoặc 'crafted'
    tier            VARCHAR(20),                  -- NULL (normal), silver/gold/titan/diamond (crafted)
    upgrade_level   SMALLINT NOT NULL DEFAULT 0,  -- 0 → 16
    gem_slot_1      UUID REFERENCES player_gems(gem_id),
    gem_slot_2      UUID REFERENCES player_gems(gem_id),  -- NULL cho trang bị thường
    is_equipped     BOOLEAN NOT NULL DEFAULT FALSE,
    equipped_on     VARCHAR(50),                  -- character_id đang mặc
    is_locked       BOOLEAN NOT NULL DEFAULT FALSE, -- lock khi equip
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT valid_upgrade CHECK (upgrade_level >= 0 AND upgrade_level <= 16)
);

-- Log đá đã dùng nâng cấp (để tính khi tách)
CREATE TABLE IF NOT EXISTS equipment_upgrade_log (
    log_id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    equipment_id    UUID NOT NULL REFERENCES player_equipment(equipment_id),
    from_level      SMALLINT NOT NULL,
    to_level        SMALLINT NOT NULL,
    stone_level     SMALLINT NOT NULL,
    success         BOOLEAN NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Ngọc đang sở hữu
CREATE TABLE IF NOT EXISTS player_gems (
    gem_id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id       VARCHAR(50) NOT NULL REFERENCES player_profiles(player_id),
    gem_type        VARCHAR(20) NOT NULL,  -- hp/damage/defense/critical
    gem_level       SMALLINT NOT NULL,     -- 1 → 10
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT valid_gem_level CHECK (gem_level >= 1 AND gem_level <= 10)
);

-- Đá nâng cấp đang sở hữu
CREATE TABLE IF NOT EXISTS player_stones (
    stone_id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id       VARCHAR(50) NOT NULL REFERENCES player_profiles(player_id),
    stone_level     SMALLINT NOT NULL,     -- 1 → 12
    quantity        INT NOT NULL DEFAULT 1,
    CONSTRAINT valid_stone_level CHECK (stone_level >= 1 AND stone_level <= 12)
);

-- Nguyên liệu chế tạo đang sở hữu
CREATE TABLE IF NOT EXISTS player_materials (
    player_id       VARCHAR(50) NOT NULL REFERENCES player_profiles(player_id),
    material_id     VARCHAR(100) NOT NULL,
    quantity        INT NOT NULL DEFAULT 0,
    PRIMARY KEY (player_id, material_id)
);

-- Config trang bị (admin editable)
CREATE TABLE IF NOT EXISTS config_equipment_items (
    item_id         VARCHAR(100) PRIMARY KEY,
    name            VARCHAR(200) NOT NULL,
    slot            VARCHAR(20) NOT NULL,
    category        VARCHAR(20) NOT NULL,       -- normal / crafted
    tier            VARCHAR(20),                -- NULL / silver / gold / titan / diamond
    required_level  INT NOT NULL,
    character_id    VARCHAR(50),                -- NULL = all characters (crafted)
    gem_slots       SMALLINT NOT NULL DEFAULT 1,
    stat_hp         INT NOT NULL DEFAULT 0,
    stat_damage     INT NOT NULL DEFAULT 0,
    stat_defense    INT NOT NULL DEFAULT 0,
    stat_crit       NUMERIC(5,2) NOT NULL DEFAULT 0,
    stat_move_energy INT NOT NULL DEFAULT 0,
    price_coin      INT DEFAULT 0,
    price_gem       INT DEFAULT 0,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE
);

-- Config công thức chế tạo
CREATE TABLE IF NOT EXISTS config_crafting_recipes (
    recipe_id       VARCHAR(100) PRIMARY KEY,
    result_item_id  VARCHAR(100) NOT NULL REFERENCES config_equipment_items(item_id),
    materials       JSONB NOT NULL,  -- [{"material_id":"silver_ore","quantity":5}, ...]
    is_active       BOOLEAN NOT NULL DEFAULT TRUE
);

-- Config nguyên liệu
CREATE TABLE IF NOT EXISTS config_materials (
    material_id     VARCHAR(100) PRIMARY KEY,
    name            VARCHAR(200) NOT NULL,
    description     TEXT,
    source          VARCHAR(20) NOT NULL,  -- 'drop' hoặc 'gem_shop'
    price_gem       INT DEFAULT 0,         -- giá nếu mua bằng gem
    tier            VARCHAR(20) NOT NULL,  -- silver/gold/titan/diamond
    is_active       BOOLEAN NOT NULL DEFAULT TRUE
);

-- Config tỷ lệ nâng cấp (admin editable)
CREATE TABLE IF NOT EXISTS config_upgrade_rates (
    from_level      SMALLINT NOT NULL,  -- 0 → 15
    to_level        SMALLINT NOT NULL,  -- 1 → 16
    base_rate       NUMERIC(5,2) NOT NULL,  -- % tỷ lệ base
    min_stone_level SMALLINT NOT NULL,      -- cấp đá tối thiểu
    fail_reset_to   SMALLINT NOT NULL,      -- quay về level nào khi fail
    PRIMARY KEY (from_level, to_level)
);

-- Config set bonus
CREATE TABLE IF NOT EXISTS config_set_bonuses (
    tier            VARCHAR(20) NOT NULL,  -- silver/gold/titan/diamond
    pieces_required SMALLINT NOT NULL,     -- 2/4/6
    bonus_hp_pct    NUMERIC(5,2) NOT NULL DEFAULT 0,
    bonus_dmg_pct   NUMERIC(5,2) NOT NULL DEFAULT 0,
    bonus_def_pct   NUMERIC(5,2) NOT NULL DEFAULT 0,
    bonus_crit_pct  NUMERIC(5,2) NOT NULL DEFAULT 0,
    PRIMARY KEY (tier, pieces_required)
);

-- Config ngọc
CREATE TABLE IF NOT EXISTS config_gems (
    gem_type        VARCHAR(20) NOT NULL,
    gem_level       SMALLINT NOT NULL,
    stat_value      NUMERIC(10,2) NOT NULL,  -- flat value hoặc %
    PRIMARY KEY (gem_type, gem_level)
);

-- Config đá nâng cấp
CREATE TABLE IF NOT EXISTS config_stones (
    stone_level     SMALLINT PRIMARY KEY,
    bonus_rate      NUMERIC(5,2) NOT NULL,  -- % bonus thêm khi dùng nâng cấp
    price_coin      INT DEFAULT 0,
    price_gem       INT DEFAULT 0,
    source          VARCHAR(20) NOT NULL    -- 'coin_shop', 'gem_shop', 'merge_only'
);
```

---

## 17. API endpoints

### Equipment

```
GET    /player/equipment                    -- Danh sách trang bị sở hữu
POST   /player/equipment/equip              -- Mặc trang bị {equipmentId, characterId}
POST   /player/equipment/unequip            -- Tháo trang bị {equipmentId}
POST   /player/equipment/upgrade            -- Nâng cấp {equipmentId, stoneId}
POST   /player/equipment/dismantle          -- Tách trang bị {equipmentId}
POST   /player/equipment/socket             -- Khảm ngọc {equipmentId, slotIndex, gemId}
POST   /player/equipment/unsocket           -- Tháo ngọc {equipmentId, slotIndex}
```

### Crafting

```
GET    /crafting/recipes                    -- Danh sách công thức
POST   /crafting/craft                      -- Chế tạo {recipeId}
```

### Shop (mở rộng)

```
GET    /shop/equipment?characterId=xxx      -- Shop trang bị theo nhân vật
POST   /shop/equipment/buy                  -- Mua trang bị {itemId}
GET    /shop/stones                         -- Shop đá nâng cấp
POST   /shop/stones/buy                     -- Mua đá {stoneLevel, quantity}
GET    /shop/gems                           -- Shop ngọc
POST   /shop/gems/buy                       -- Mua ngọc {gemType, gemLevel, quantity}
GET    /shop/materials                      -- Shop nguyên liệu (gem)
POST   /shop/materials/buy                  -- Mua nguyên liệu {materialId, quantity}
```

### Merge

```
POST   /merge/stone                         -- Ghép đá {stoneLevel, count}
POST   /merge/gem                           -- Ghép ngọc {gemType, gemLevel, count}
```

### Inventory (mở rộng)

```
GET    /player/stones                       -- Danh sách đá sở hữu
GET    /player/gems                         -- Danh sách ngọc sở hữu
GET    /player/materials                    -- Danh sách nguyên liệu sở hữu
```

---

## 18. Admin config

### Game Config trong Admin Dashboard

Các bảng config cần UI quản lý:

| Config | Bảng | Mô tả |
|--------|------|-------|
| Equipment Items | `config_equipment_items` | CRUD trang bị, stat, giá |
| Crafting Recipes | `config_crafting_recipes` | CRUD công thức chế tạo |
| Materials | `config_materials` | CRUD nguyên liệu, nguồn, giá gem |
| Upgrade Rates | `config_upgrade_rates` | Tỷ lệ nâng cấp mỗi level |
| Set Bonuses | `config_set_bonuses` | Bonus khi đủ set |
| Gem Stats | `config_gems` | Chỉ số ngọc mỗi cấp |
| Stone Config | `config_stones` | Bonus %, giá đá mỗi cấp |
| Merge Rate | `game_settings` key `merge_rate` | Tỷ lệ ghép (mặc định 50%) |
| Milestone Bonus | `game_settings` key `upgrade_milestones` | % bonus tại mốc 6/10/14/16 |
