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

# --- 2. ПОДКЛЮЧЕНИЕ К ТАБЛИЦАМ ---
def get_sheets():
    try:
        scope = ["https://spreadsheets.google.com/feeds", "https://www.googleapis.com/auth/drive"]
        creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
        gc = gspread.authorize(creds)
        doc = gc.open(SHEET_NAME)
        return doc.sheet1
    except: return None

sheet = get_sheets()
bot = telebot.TeleBot(BOT_TOKEN)
u_data = {} 

# --- 3. ДИНАМИЧЕСКОЕ МЕНЮ ---

@bot.message_handler(commands=['start', 'profile'])
@bot.message_handler(func=lambda m: m.text == "👤 Мой профиль")
def welcome_and_profile(message):
    uid = str(message.from_user.id)
    user_row = None
    
    try:
        cell = sheet.find(uid, in_column=2)
        if cell:
            user_row = sheet.row_values(cell.row)
    except: pass

    # Создаем клавиатуру
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
    
    if user_row:
        # Если аккаунт ЕСТЬ: убираем регистрацию
        markup.row("👤 Мой профиль", "💸 Перевод")
        if int(uid) in ADMIN_LIST:
            markup.row("⚙️ Админ-панель")
            
        text = (f"👤 **Профиль: {user_row[2]}**\n"
                f"💼 Должность: {user_row[4]}\n"
                f"💰 Баланс: **{user_row[3]} Gold**\n"
                f"🆔 Код: `{user_row[0]}`")
        
        # Кнопки управления голдой (Инлайн)
        kb = telebot.types.InlineKeyboardMarkup()
        kb.row(
            telebot.types.InlineKeyboardButton("📉 Снять Gold", callback_data="pre_withdraw"),
            telebot.types.InlineKeyboardButton("💸 Перевод", callback_data="pre_transfer")
        )
        bot.send_message(message.chat.id, text, parse_mode="Markdown", reply_markup=markup)
        bot.send_message(message.chat.id, "Выберите финансовую операцию:", reply_markup=kb)
    else:
        # Если аккаунта НЕТ: только кнопка регистрации
        markup.row("📝 Регистрация")
        bot.send_message(message.chat.id, "👋 Привет! Твой аккаунт не найден. Пройди регистрацию:", reply_markup=markup)

# --- 4. ЛОГИКА ПЕРЕВОДА ---

@bot.message_handler(func=lambda m: m.text == "💸 Перевод")
@bot.callback_query_handler(func=lambda c: c.data == "pre_transfer")
def transfer_init(obj):
    # Работает и на текстовую кнопку, и на инлайн-кнопку
    chat_id = obj.chat.id if hasattr(obj, 'chat') else obj.message.chat.id
    msg = bot.send_message(chat_id, "Введите часть Ника игрока для поиска:")
    bot.register_next_step_handler(msg, search_recipient)

def search_recipient(m):
    query = m.text.lower()
    try:
        all_players = sheet.get_all_values()[1:]
        found = [p for p in all_players if query in p[2].lower() and p[1] != str(m.from_user.id)]
        
        if not found:
            return bot.send_message(m.chat.id, "❌ Никто не найден.")
        
        kb = telebot.types.InlineKeyboardMarkup()
        for p in found[:8]:
            kb.add(telebot.types.InlineKeyboardButton(f"{p[2]} ({p[4]})", callback_data=f"tr_{p[1]}"))
        bot.send_message(m.chat.id, "Выберите получателя:", reply_markup=kb)
    except: bot.send_message(m.chat.id, "Ошибка базы данных.")

@bot.callback_query_handler(func=lambda c: c.data.startswith("tr_"))
def ask_amount(c):
    u_data[c.from_user.id] = {'target_id': c.data.split("_")[1]}
    bot.edit_message_text("Введите сумму Gold для перевода:", c.message.chat.id, c.message.message_id)
    bot.register_next_step_handler(c.message, process_transfer)

def process_transfer(m):
    try:
        amt = float(m.text.replace(',', '.'))
        if amt <= 0: return bot.send_message(m.chat.id, "❌ Сумма должна быть > 0")
        
        s_cell = sheet.find(str(m.from_user.id), in_column=2)
        t_cell = sheet.find(u_data[m.from_user.id]['target_id'], in_column=2)
        
        s_row = sheet.row_values(s_cell.row)
        t_row = sheet.row_values(t_cell.row)
        
        s_bal = float(s_row[3])
        if s_bal < amt: return bot.send_message(m.chat.id, f"❌ Недостаточно Gold (Баланс: {s_bal})")
        
        sheet.update_cell(s_cell.row, 4, str(s_bal - amt))
        sheet.update_cell(t_cell.row, 4, str(float(t_row[3]) + amt))
        
        bot.send_message(m.chat.id, f"✅ Вы успешно перевели {amt} Gold игроку {t_row[2]}.")
        bot.send_message(t_row[1], f"💰 Вам поступил перевод от {s_row[2]}: +{amt} Gold")
    except: bot.send_message(m.chat.id, "❌ Ошибка. Введите число.")

# --- 5. СНЯТИЕ И АДМИНКА ---

@bot.callback_query_handler(func=lambda c: c.data == "pre_withdraw")
def withdraw_init(c):
    msg = bot.send_message(c.message.chat.id, "Сколько Gold вы хотите снять?")
    bot.register_next_step_handler(msg, send_to_adm)

def send_to_adm(m):
    try:
        amt = float(m.text)
        kb = telebot.types.InlineKeyboardMarkup().add(telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"ok_{m.from_user.id}_{amt}"))
        for adm in ADMIN_LIST:
            bot.send_message(adm, f"🚨 Заявка на вывод: {m.from_user.first_name}\nСумма: {amt} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Заявка отправлена админам.")
    except: bot.send_message(m.chat.id, "❌ Введите число.")

@bot.callback_query_handler(func=lambda c: c.data.startswith("ok_"))
def approve_payout(c):
    _, u_id, amt = c.data.split("_")
    try:
        cell = sheet.find(u_id, in_column=2)
        row = sheet.row_values(cell.row)
        new_bal = float(row[3]) - float(amt)
        sheet.update_cell(cell.row, 4, str(new_bal))
        bot.edit_message_text(f"✅ Выплачено {amt} для {row[2]}", c.message.chat.id, c.message.message_id)
        bot.send_message(u_id, f"✅ Твой вывод на {amt} Gold одобрен!")
    except: bot.send_message(c.message.chat.id, "❌ Ошибка при выплате.")

@bot.message_handler(func=lambda m: m.text == "📝 Регистрация")
def reg_final(m):
    try:
        if sheet.find(str(m.from_user.id), in_column=2): return
        pwd = ''.join(random.choices(string.ascii_letters + string.digits, k=12))
        sheet.append_row([pwd, str(m.from_user.id), m.from_user.first_name, "0", "Игрок"])
        bot.send_message(m.chat.id, f"✅ Аккаунт создан! Теперь напиши /start для обновления меню.")
    except: bot.send_message(m.chat.id, "❌ Ошибка регистрации.")

# --- 6. ЗАПУСК ---
app = Flask(__name__)
@app.route('/')
def h(): return "OK", 200

if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    bot.remove_webhook()
    time.sleep(1)
    try: bot.send_message(NOTIFY_USER_ID, "🚀 Бот запущен")
    except: pass
    bot.infinity_polling(none_stop=True, skip_pending=True)
