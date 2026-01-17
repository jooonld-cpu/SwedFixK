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

# Твой ID подтвержден, так как уведомления приходят
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
    uid = message.from_user.id
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
    
    # Если ты админ — добавляем тебе скрытую кнопку настроек прямо в меню
    if uid in ADMIN_LIST:
        markup.add("📝 Регистрация", "👤 Мой профиль")
        markup.add("⚙️ Настройки (Админ)")
    else:
        markup.add("📝 Регистрация", "👤 Мой профиль")
        
    bot.send_message(message.chat.id, "👋 Добро пожаловать в SwedenFINK!\nВыберите нужное действие:", reply_markup=markup)

# Обработка и команды /config, и текстовой кнопки для надежности
@bot.message_handler(commands=['config'])
@bot.message_handler(func=lambda m: m.text == "⚙️ Настройки (Админ)")
def admin_config(message):
    uid = message.from_user.id
    if uid not in ADMIN_LIST:
        return 

    kb = telebot.types.InlineKeyboardMarkup()
    kb.row(telebot.types.InlineKeyboardButton("📊 Статистика", callback_data="adm_stats"))
    kb.row(telebot.types.InlineKeyboardButton("📢 Рассылка", callback_data="adm_broadcast"))
    kb.row(telebot.types.InlineKeyboardButton("💰 Изменить баланс", callback_data="adm_edit_bal"))
    
    bot.send_message(message.chat.id, "🛠 **Панель управления SwedenFINK**", parse_mode="Markdown", reply_markup=kb)

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
        msg = bot.send_message(c.message.chat.id, "Введите сумму для снятия:")
        bot.register_next_step_handler(msg, process_withdraw_request)
    elif c.data == "refresh_profile":
        bot.delete_message(c.message.chat.id, c.message.message_id)
        show_profile(c.message)
    elif c.data == "adm_stats":
        count = len(sheet.get_all_values()) - 1
        bot.send_message(c.message.chat.id, f"📊 Всего игроков в базе: {count}")
    elif c.data == "adm_broadcast":
        msg = bot.send_message(c.message.chat.id, "Введите текст рассылки для всех:")
        bot.register_next_step_handler(msg, start_broadcast)
    elif c.data == "adm_edit_bal":
        msg = bot.send_message(c.message.chat.id, "Введите 12-значный ID игрока:")
        bot.register_next_step_handler(msg, admin_find_user_for_bal)
    elif c.data.startswith("adm_ok_"):
        _, _, r_idx, amt = c.data.split("_")
        execute_payout(c, int(r_idx), float(amt))
    elif c.data == "adm_no":
        bot.edit_message_text("❌ Заявка отклонена.", c.message.chat.id, c.message.message_id)
    bot.answer_callback_query(c.id)

# --- АДМИН ФУНКЦИИ ---
def admin_find_user_for_bal(m):
    try:
        cell = sheet.find(m.text.strip(), in_column=1)
        row = sheet.row_values(cell.row)
        u_data[m.from_user.id] = {'edit_row': cell.row}
        msg = bot.send_message(m.chat.id, f"👤 Игрок: {row[2]}\n💰 Текущий баланс: {row[3]}\n\nВведите новое число баланса:")
        bot.register_next_step_handler(msg, admin_save_new_bal)
    except: bot.send_message(m.chat.id, "❌ ID не найден в системе.")

def admin_save_new_bal(m):
    try:
        new_val = m.text.replace(',', '.')
        sheet.update_cell(u_data[m.from_user.id]['edit_row'], 4, new_val)
        bot.send_message(m.chat.id, f"✅ Баланс успешно обновлен на {new_val}")
    except: bot.send_message(m.chat.id, "❌ Ошибка. Введите число.")

def start_broadcast(m):
    all_data = sheet.get_all_values()[1:]
    sent = 0
    for row in all_data:
        try: 
            bot.send_message(row[1], f"📢 **Оповещение от администрации:**\n\n{m.text}", parse_mode="Markdown")
            sent += 1
        except: continue
    bot.send_message(m.chat.id, f"✅ Рассылка завершена. Получили: {sent}")

def process_withdraw_request(m):
    try:
        amt = float(m.text.replace(',', '.'))
        cell = sheet.find(str(m.from_user.id), in_column=2)
        row = sheet.row_values(cell.row)
        if float(str(row[3]).replace(',', '.')) < amt: return bot.send_message(m.chat.id, "❌ Недостаточно средств.")
        kb = telebot.types.InlineKeyboardMarkup().add(
            telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"adm_ok_{cell.row}_{amt}"),
            telebot.types.InlineKeyboardButton("❌ Отклонить", callback_data="adm_no"))
        for adm in ADMIN_LIST:
            bot.send_message(adm, f"🚨 **ЗАЯВКА НА ВЫВОД**\n👤 {row[2]}\n💰 {amt} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Заявка отправлена администраторам.")
    except: bot.send_message(m.chat.id, "❌ Введите корректное число.")

def execute_payout(c, r_idx, amt):
    try:
        row = sheet.row_values(r_idx)
        new_bal = float(str(row[3]).replace(',', '.')) - amt
        sheet.update_cell(r_idx, 4, str(new_bal))
        if history_sheet: 
            history_sheet.append_row([datetime.now().strftime("%d.%m %H:%M"), row[2], c.from_user.first_name, amt])
        bot.edit_message_text(f"✅ Выплачено {amt} Gold пользователю {row[2]}", c.message.chat.id, c.message.message_id)
        bot.send_message(row[1], f"✅ Ваш вывод {amt} Gold одобрен!")
    except: bot.send_message(c.message.chat.id, "❌ Ошибка при обновлении баланса.")

# --- 5. ВЕБ-СЕРВЕР ---
app = Flask(__name__)
@app.route('/')
def health(): return "OK", 200

# --- 6. ЗАПУСК ---
if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    while True:
        try:
            bot.remove_webhook()
            bot.infinity_polling(none_stop=True, skip_pending=True)
        except Exception as e:
            time.sleep(5)

