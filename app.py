import os
import time
import json
import telebot
import gspread
import random
import string
from oauth2client.service_account import ServiceAccountCredentials
from datetime import datetime
from flask import Flask
from threading import Thread

# --- 1. НАСТРОЙКИ ---
BOT_TOKEN = os.getenv("BOT_TOKEN")
SHEET_NAME = os.getenv("SHEET_NAME", "SwedenFINK")
GCP_JSON_DATA = os.getenv("GCP_JSON")

# ВНИМАНИЕ: Проверь свои ID еще раз. 
# Можно узнать свой ID, написав боту @userinfobot
ADMIN_LIST = [7631664265, 6343896085]

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
        wks_list = [w.title for w in doc.worksheets()]
        h_sheet = doc.worksheet("История") if "История" in wks_list else None
        print("✅ Таблицы подключены", flush=True)
        return doc.sheet1, h_sheet
    except Exception as e:
        print(f"❌ Ошибка Google: {e}", flush=True)
        return None, None

sheet, history_sheet = get_sheets()
bot = telebot.TeleBot(BOT_TOKEN)
u_data = {}

# --- 3. ФУНКЦИИ ---
def gen_id():
    while True:
        nid = ''.join(random.choice(string.ascii_letters + string.digits) for _ in range(12))
        try:
            if not sheet.find(nid, in_column=1): return nid
        except: return nid

# --- 4. ОБРАБОТЧИКИ ---

@bot.message_handler(commands=['start'])
def welcome(message):
    print(f"DEBUG: Пользователь {message.from_user.id} нажал /start", flush=True)
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
    markup.add("📝 Регистрация", "👤 Мой профиль")
    bot.send_message(message.chat.id, "👋 Добро пожаловать в SwedenFINK!\nИспользуйте меню ниже:", reply_markup=markup)

# --- ТЕХНИЧЕСКОЕ МЕНЮ АДМИНА (/config) ---
@bot.message_handler(commands=['config'])
def admin_config(message):
    uid = message.from_user.id
    print(f"DEBUG: Попытка доступа к /config от {uid}", flush=True)
    
    if uid not in ADMIN_LIST:
        print(f"DEBUG: Отказ в доступе для {uid}. Его нет в {ADMIN_LIST}", flush=True)
        return 

    kb = telebot.types.InlineKeyboardMarkup()
    kb.row(telebot.types.InlineKeyboardButton("📊 Статистика", callback_data="adm_stats"))
    kb.row(telebot.types.InlineKeyboardButton("📢 Рассылка", callback_data="adm_broadcast"))
    kb.row(telebot.types.InlineKeyboardButton("💰 Изменить баланс", callback_data="adm_edit_bal"))
    
    bot.send_message(message.chat.id, "⚙️ **Панель управления администратора**", parse_mode="Markdown", reply_markup=kb)

# --- РЕГИСТРАЦИЯ ---
@bot.message_handler(func=lambda m: m.text == "📝 Регистрация")
def reg(m):
    try:
        if sheet.find(str(m.from_user.id), in_column=2):
            return bot.send_message(m.chat.id, "❌ Вы уже зарегистрированы!")
        msg = bot.send_message(m.chat.id, "Введите ваш игровой Ник:")
        bot.register_next_step_handler(msg, get_nick)
    except: bot.send_message(m.chat.id, "⚠️ Ошибка связи с таблицей.")

def get_nick(m):
    u_data[m.from_user.id] = {'n': m.text}
    msg = bot.send_message(m.chat.id, "Введите вашу Должность:")
    bot.register_next_step_handler(msg, get_job)

def get_job(m):
    uid = m.from_user.id
    try:
        pwd = gen_id()
        sheet.append_row([pwd, str(uid), u_data[uid]['n'], "0", m.text])
        bot.send_message(m.chat.id, f"✅ Успешно! Ваш ID: `{pwd}`")
    except Exception as e: bot.send_message(m.chat.id, f"❌ Ошибка: {e}")

# --- ПРОФИЛЬ ---
@bot.message_handler(func=lambda m: m.text == "👤 Мой профиль")
def show_profile(m):
    try:
        cell = sheet.find(str(m.from_user.id), in_column=2)
        if not cell: return bot.send_message(m.chat.id, "❌ Вы не зарегистрированы.")
        row = sheet.row_values(cell.row)
        text = (f"👤 **Профиль: {row[2]}**\n💼 Должность: {row[4]}\n💰 Баланс: **{row[3]} Gold**\n🆔 Код: `{row[0]}`")
        kb = telebot.types.InlineKeyboardMarkup()
        kb.row(telebot.types.InlineKeyboardButton("📉 Снять Gold", callback_data="pre_withdraw"))
        kb.row(telebot.types.InlineKeyboardButton("🔄 Обновить", callback_data="refresh_profile"))
        bot.send_message(m.chat.id, text, parse_mode="Markdown", reply_markup=kb)
    except: bot.send_message(m.chat.id, "⚠️ Ошибка загрузки данных.")

# --- CALLBACKS ---
@bot.callback_query_handler(func=lambda c: True)
def handle_callback(c):
    if c.data == "pre_withdraw":
        msg = bot.send_message(c.message.chat.id, "Сумма для снятия:")
        bot.register_next_step_handler(msg, process_withdraw_request)
    elif c.data == "refresh_profile":
        bot.delete_message(c.message.chat.id, c.message.message_id)
        show_profile(c.message)
    elif c.data == "adm_stats":
        count = len(sheet.get_all_values()) - 1
        bot.send_message(c.message.chat.id, f"📊 Игроков: {count}")
    elif c.data == "adm_broadcast":
        msg = bot.send_message(c.message.chat.id, "Текст рассылки:")
        bot.register_next_step_handler(msg, start_broadcast)
    elif c.data == "adm_edit_bal":
        msg = bot.send_message(c.message.chat.id, "12-значный ID игрока:")
        bot.register_next_step_handler(msg, admin_find_user_for_bal)
    elif c.data.startswith("adm_ok_"):
        _, _, r_idx, amt = c.data.split("_")
        execute_payout(c, int(r_idx), float(amt))
    elif c.data == "adm_no":
        bot.edit_message_text("❌ Отклонено.", c.message.chat.id, c.message.message_id)
    bot.answer_callback_query(c.id)

# --- АДМИН ФУНКЦИИ ---
def admin_find_user_for_bal(m):
    try:
        cell = sheet.find(m.text.strip(), in_column=1)
        row = sheet.row_values(cell.row)
        u_data[m.from_user.id] = {'edit_row': cell.row}
        msg = bot.send_message(m.chat.id, f"👤 Игрок: {row[2]}\n💰 Баланс: {row[3]}\n\nНовое значение:")
        bot.register_next_step_handler(msg, admin_save_new_bal)
    except: bot.send_message(m.chat.id, "❌ ID не найден.")

def admin_save_new_bal(m):
    try:
        new_val = m.text.replace(',', '.')
        sheet.update_cell(u_data[m.from_user.id]['edit_row'], 4, new_val)
        bot.send_message(m.chat.id, f"✅ Баланс обновлен на {new_val}")
    except: bot.send_message(m.chat.id, "❌ Ошибка числа.")

def start_broadcast(m):
    all_data = sheet.get_all_values()[1:]
    for row in all_data:
        try: bot.send_message(row[1], f"📢 **Оповещение:**\n\n{m.text}", parse_mode="Markdown")
        except: continue
    bot.send_message(m.chat.id, "✅ Рассылка завершена.")

def process_withdraw_request(m):
    try:
        amt = float(m.text.replace(',', '.'))
        cell = sheet.find(str(m.from_user.id), in_column=2)
        row = sheet.row_values(cell.row)
        if float(str(row[3]).replace(',', '.')) < amt: return bot.send_message(m.chat.id, "❌ Мало Gold.")
        kb = telebot.types.InlineKeyboardMarkup().add(
            telebot.types.InlineKeyboardButton("✅ Да", callback_data=f"adm_ok_{cell.row}_{amt}"),
            telebot.types.InlineKeyboardButton("❌ Нет", callback_data="adm_no"))
        for adm in ADMIN_LIST:
            bot.send_message(adm, f"🚨 **ЗАЯВКА**\n👤 {row[2]}\n💰 {amt} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Отправлено.")
    except: bot.send_message(m.chat.id, "❌ Введите число.")

def execute_payout(c, r_idx, amt):
    try:
        row = sheet.row_values(r_idx)
        new_bal = float(str(row[3]).replace(',', '.')) - amt
        sheet.update_cell(r_idx, 4, str(new_bal))
        if history_sheet: history_sheet.append_row([datetime.now().strftime("%d.%m %H:%M"), row[2], c.from_user.first_name, amt])
        bot.edit_message_text(f"✅ Выплачено {amt}", c.message.chat.id, c.message.message_id)
        bot.send_message(row[1], f"✅ Вывод {amt} Gold одобрен!")
    except: bot.send_message(c.message.chat.id, "❌ Ошибка БД.")

# --- 5. ВЕБ-СЕРВЕР ---
app = Flask(__name__)
@app.route('/')
def health(): return "OK", 200

# --- 6. ЗАПУСК ---
if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    print("🚀 Бот запускается...", flush=True)
    while True:
        try:
            bot.remove_webhook()
            bot.infinity_polling(none_stop=True, skip_pending=True)
        except Exception as e:
            print(f"🔄 Ошибка: {e}", flush=True)
            time.sleep(5)

