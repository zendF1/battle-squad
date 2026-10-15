UPDATE game_settings
SET value = ((((((value::jsonb #- '{tiers,bronze,hitChance}') #- '{tiers,silver,hitChance}') #- '{tiers,gold,hitChance}') #- '{tiers,platinum,hitChance}') #- '{tiers,diamond,hitChance}') #- '{tiers,master,hitChance}')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty';
