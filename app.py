import os
import time
import telebot
import gspread
import random
import string
from oauth2client.service_account import ServiceAccountCredentials
from flask import Flask
from threading import Thread

# --- 1. НАСТРОЙКИ ---
BOT_TOKEN = os.getenv("BOT_TOKEN")
SHEET_NAME = os.getenv("SHEET_NAME", "SwedenFINK")
GCP_JSON_DATA = os.getenv("GCP_JSON")

ADMIN_LIST = [7631664265, 6343896085]
NOTIFY_USER_ID = 7631664265 

if GCP_JSON_DATA:
    with open("credentials.json", "w") as f:
        f.write(GCP_JSON_DATA)

def get_sheets():
    try:
        scope = ["https://spreadsheets.google.com/feeds", "https://www.googleapis.com/auth/drive"]
        creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
        gc = gspread.authorize(creds)
        return gc.open(SHEET_NAME).sheet1
    except: return None

sheet = get_sheets()
bot = telebot.TeleBot(BOT_TOKEN)
u_data = {} 

# --- 2. ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ (ОПТИМИЗАЦИЯ) ---

def get_user_from_db(tg_id):
    """Быстрый поиск пользователя без лишних запросов к API Google"""
    try:
        all_data = sheet.get_all_values()
        for idx, row in enumerate(all_data):
            if row[1] == str(tg_id):
                return row, idx + 1 # Возвращаем данные строки и её номер (1-based)
        return None, None
    except: return None, None

# --- 3. ОСНОВНАЯ ЛОГИКА МЕНЮ ---

@bot.message_handler(commands=['start', 'profile'])
@bot.message_handler(func=lambda m: m.text == "👤 Мой профиль")
def show_profile(message):
    uid = message.from_user.id
    user_row, _ = get_user_from_db(uid)
    
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
    
    if user_row:
        # Если зарегистрирован: убираем регистрацию из кнопок
        markup.row("👤 Мой профиль", "💸 Перевод")
        if uid in ADMIN_LIST:
            markup.row("⚙️ Админ-панель")
            
        text = (f"👤 **Профиль: {user_row[2]}**\n"
                f"💼 Должность: {user_row[4]}\n"
                f"💰 Баланс: **{user_row[3]} Gold**\n"
                f"🆔 Твой код: `{user_row[0]}`")
        
        # Кнопки под сообщением (Снять и Перевести в одном месте)
        kb = telebot.types.InlineKeyboardMarkup()
        kb.row(
            telebot.types.InlineKeyboardButton("📉 Снять Gold", callback_data="pre_withdraw"),
            telebot.types.InlineKeyboardButton("💸 Перевод", callback_data="pre_transfer")
        )
        bot.send_message(message.chat.id, text, parse_mode="Markdown", reply_markup=markup)
        bot.send_message(message.chat.id, "Управление средствами:", reply_markup=kb)
    else:
        # Если НЕ зарегистрирован
        markup.row("📝 Регистрация")
        bot.send_message(message.chat.id, "👋 Привет! Ты не зарегистрирован. Нажми кнопку ниже:", reply_markup=markup)

# --- 4. ЛОГИКА ПЕРЕВОДА ---

@bot.callback_query_handler(func=lambda c: c.data == "pre_transfer")
@bot.message_handler(func=lambda m: m.text == "💸 Перевод")
def transfer_init(obj):
    chat_id = obj.chat.id if hasattr(obj, 'chat') else obj.message.chat.id
    msg = bot.send_message(chat_id, "Введите часть Ника игрока для поиска:")
    bot.register_next_step_handler(msg, search_recipient)

def search_recipient(m):
    query = m.text.lower()
    try:
        all_players = sheet.get_all_values()[1:]
        found = [p for p in all_players if query in p[2].lower() and p[1] != str(m.from_user.id)]
        
        if not found:
            return bot.send_message(m.chat.id, "❌ Игрок не найден.")
        
        kb = telebot.types.InlineKeyboardMarkup()
        for p in found[:8]:
            kb.add(telebot.types.InlineKeyboardButton(f"{p[2]} ({p[4]})", callback_data=f"tr_{p[1]}"))
        bot.send_message(m.chat.id, "Выберите получателя:", reply_markup=kb)
    except: bot.send_message(m.chat.id, "Ошибка поиска.")

@bot.callback_query_handler(func=lambda c: c.data.startswith("tr_"))
def ask_amount(c):
    u_data[c.from_user.id] = {'target_id': c.data.split("_")[1]}
    bot.delete_message(c.message.chat.id, c.message.message_id)
    msg = bot.send_message(c.message.chat.id, "Введите сумму Gold для перевода:")
    bot.register_next_step_handler(msg, process_transfer)

def process_transfer(m):
    try:
        amount = float(m.text.replace(',', '.'))
        if amount <= 0: return bot.send_message(m.chat.id, "❌ Сумма должна быть больше 0.")
        
        # Загружаем данные отправителя и получателя
        s_row, s_idx = get_user_from_db(m.from_user.id)
        t_row, t_idx = get_user_from_db(u_data[m.from_user.id]['target_id'])
        
        s_bal = float(s_row[3].replace(',', '.'))
        if s_bal < amount:
            return bot.send_message(m.chat.id, f"❌ Недостаточно Gold. Баланс: {s_bal}")
        
        # Обновляем таблицу (API Google вызывается только здесь)
        sheet.update_cell(s_idx, 4, str(s_bal - amount))
        sheet.update_cell(t_idx, 4, str(float(t_row[3].replace(',', '.')) + amount))
        
        bot.send_message(m.chat.id, f"✅ Перевод {amount} Gold для {t_row[2]} успешно выполнен!")
        bot.send_message(t_row[1], f"💰 Вам поступил перевод от {s_row[2]}: +{amount} Gold")
    except:
        bot.send_message(m.chat.id, "❌ Ошибка. Введите число.")

# --- 5. СНЯТИЕ И АДМИНКА ---

@bot.callback_query_handler(func=lambda c: c.data == "pre_withdraw")
def withdraw_init(c):
    msg = bot.send_message(c.message.chat.id, "Какую сумму вы хотите снять?")
    bot.register_next_step_handler(msg, send_to_admin)

def send_to_admin(m):
    try:
        amt = float(m.text)
        user_row, _ = get_user_data_local(m.from_user.id)
        kb = telebot.types.InlineKeyboardMarkup().add(
            telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"ap_{m.from_user.id}_{amt}")
        )
        for adm in ADMIN_LIST:
            bot.send_message(adm, f"🚨 Заявка: {m.from_user.first_name}\nСумма: {amt} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Заявка отправлена администрации.")
    except: bot.send_message(m.chat.id, "❌ Введите число.")

@bot.callback_query_handler(func=lambda c: c.data.startswith("ap_"))
def approve_withdraw(c):
    _, uid, amt = c.data.split("_")
    try:
        user_row, row_idx = get_user_from_db(uid)
        new_bal = float(user_row[3]) - float(amt)
        sheet.update_cell(row_idx, 4, str(new_bal))
        bot.edit_message_text(f"✅ Выплата {amt} для {user_row[2]} одобрена.", c.message.chat.id, c.message.message_id)
        bot.send_message(uid, f"✅ Твой вывод на {amt} Gold одобрен!")
    except: bot.send_message(c.message.chat.id, "❌ Ошибка обновления таблицы.")

# --- 6. РЕГИСТРАЦИЯ ---

@bot.message_handler(func=lambda m: m.text == "📝 Регистрация")
def registration(m):
    if get_user_from_db(m.from_user.id)[0]:
        return bot.send_message(m.chat.id, "❌ Вы уже зарегистрированы.")
    msg = bot.send_message(m.chat.id, "Введите ваш игровой ник:")
    bot.register_next_step_handler(msg, finish_reg)

def finish_reg(m):
    pwd = ''.join(random.choices(string.ascii_letters + string.digits, k=12))
    sheet.append_row([pwd, str(m.from_user.id), m.text, "0", "Игрок"])
    bot.send_message(m.chat.id, "✅ Регистрация завершена! Напиши /start, чтобы обновить кнопки.")

# --- 7. ЗАПУСК ---
app = Flask(__name__)
@app.route('/')
def h(): return "OK", 200

if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    
    # Решение проблем с задержкой и 409 Conflict
    bot.remove_webhook()
    time.sleep(1)
    
    try:
        bot.send_message(NOTIFY_USER_ID, "🚀 Бот запущен и оптимизирован!")
    except: pass

    bot.infinity_polling(none_stop=True, skip_pending=True)

