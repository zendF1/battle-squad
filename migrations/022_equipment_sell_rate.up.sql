-- Selling a piece (POST /player/equipment/sell) pays floor(price × sell rate)
-- of its buy currency. ON CONFLICT DO NOTHING keeps admin edits on reruns.

INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('equipment.sell_rate', '0.25', 'number', 'Tỉ lệ hoàn tiền khi bán trang bị: nhận floor(giá mua × tỉ lệ), trong (0, 1]', 'equipment')
ON CONFLICT (key) DO NOTHING;
