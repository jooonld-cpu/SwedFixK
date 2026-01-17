import os
import time
import json
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

# Список ID администраторов
ADMIN_LIST = [7631664265, 6343896085]
NOTIFY_USER_ID = 7631664265 # Ваш ID для уведомления о запуске

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
    except Exception as e:
        print(f"Ошибка Google Sheets: {e}")
        return None

sheet = get_sheets()
bot = telebot.TeleBot(BOT_TOKEN)
u_data = {} # Временное хранилище для шагов перевода/админки

# --- 3. ОБРАБОТЧИКИ МЕНЮ ---

@bot.message_handler(commands=['start', 'profile'])
@bot.message_handler(func=lambda m: m.text == "👤 Мой профиль")
def welcome_and_profile(message):
    uid = str(message.from_user.id)
    
    # Создаем основную клавиатуру (кнопки внизу)
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True, one_time_keyboard=False)
    markup.row("📝 Регистрация", "👤 Мой профиль")
    markup.row("💸 Перевод")
    
    if int(uid) in ADMIN_LIST:
        markup.row("⚙️ Админ-панель")

    try:
        cell = sheet.find(uid, in_column=2)
        if not cell:
            return bot.send_message(message.chat.id, "👋 Привет! Ты еще не зарегистрирован. Нажми кнопку ниже.", reply_markup=markup)
        
        row = sheet.row_values(cell.row)
        text = (f"👤 **Профиль: {row[2]}**\n"
                f"💼 Должность: {row[4]}\n"
                f"💰 Баланс: **{row[3]} Gold**\n"
                f"🆔 Твой код: `{row[0]}`")
        
        # Инлайн-кнопка под профилем
        kb = telebot.types.InlineKeyboardMarkup()
        kb.add(telebot.types.InlineKeyboardButton("📉 Снять Gold", callback_data="pre_withdraw"))
        
        bot.send_message(message.chat.id, text, parse_mode="Markdown", reply_markup=markup)
        bot.send_message(message.chat.id, "Доступные действия с балансом:", reply_markup=kb)
    except:
        bot.send_message(message.chat.id, "👋 Ошибка загрузки данных. Попробуй /start", reply_markup=markup)

# --- 4. ЛОГИКА ПЕРЕВОДА (МЕЖДУ ИГРОКАМИ) ---

@bot.message_handler(commands=['transfer'])
@bot.message_handler(func=lambda m: m.text == "💸 Перевод")
def transfer_start(m):
    msg = bot.send_message(m.chat.id, "Введите часть Ника игрока, которому хотите перевести Gold:")
    bot.register_next_step_handler(msg, search_recipient)

def search_recipient(m):
    query = m.text.lower()
    try:
        all_players = sheet.get_all_values()[1:] # Пропускаем заголовок
        # Ищем совпадения в 3-м столбце (Ник), исключая себя
        found = [p for p in all_players if query in p[2].lower() and p[1] != str(m.from_user.id)]
        
        if not found:
            return bot.send_message(m.chat.id, "❌ Игрок не найден. Попробуйте еще раз.")
        
        kb = telebot.types.InlineKeyboardMarkup()
        for p in found[:8]: # Ограничиваем список 8 результатами
            kb.add(telebot.types.InlineKeyboardButton(f"{p[2]} ({p[4]})", callback_data=f"tr_{p[1]}"))
        
        bot.send_message(m.chat.id, "Выберите получателя из списка:", reply_markup=kb)
    except:
        bot.send_message(m.chat.id, "⚠️ Ошибка при поиске игроков.")

@bot.callback_query_handler(func=lambda c: c.data.startswith("tr_"))
def ask_transfer_amount(c):
    target_id = c.data.split("_")[1]
    u_data[c.from_user.id] = {'target_id': target_id}
    
    bot.delete_message(c.message.chat.id, c.message.message_id)
    msg = bot.send_message(c.message.chat.id, "Введите сумму перевода (только число):")
    bot.register_next_step_handler(msg, process_transfer_final)

def process_transfer_final(m):
    try:
        amount = float(m.text.replace(',', '.'))
        if amount <= 0: return bot.send_message(m.chat.id, "❌ Сумма должна быть больше нуля.")
        
        # Ищем отправителя и получателя
        s_cell = sheet.find(str(m.from_user.id), in_column=2)
        t_cell = sheet.find(u_data[m.from_user.id]['target_id'], in_column=2)
        
        s_row = sheet.row_values(s_cell.row)
        t_row = sheet.row_values(t_cell.row)
        
        s_bal = float(s_row[3].replace(',', '.'))
        
        if s_bal < amount:
            return bot.send_message(m.chat.id, f"❌ Недостаточно Gold. Твой баланс: {s_bal}")
        
        # Выполняем транзакцию
        sheet.update_cell(s_cell.row, 4, str(s_bal - amount))
        sheet.update_cell(t_cell.row, 4, str(float(t_row[3].replace(',', '.')) + amount))
        
        # Уведомления
        bot.send_message(m.chat.id, f"✅ Вы перевели {amount} Gold игроку {t_row[2]}.")
        bot.send_message(t_row[1], f"💰 Вам поступил перевод!\n👤 Отправитель: {s_row[2]}\n➕ Сумма: {amount} Gold")
    except:
        bot.send_message(m.chat.id, "❌ Ошибка. Введите корректное число.")

# --- 5. РЕГИСТРАЦИЯ ---

@bot.message_handler(commands=['registration'])
@bot.message_handler(func=lambda m: m.text == "📝 Регистрация")
def registration_start(m):
    if sheet.find(str(m.from_user.id), in_column=2):
        return bot.send_message(m.chat.id, "❌ Вы уже зарегистрированы.")
    msg = bot.send_message(m.chat.id, "Введите ваш Игровой Ник:")
    bot.register_next_step_handler(msg, registration_finish)

def registration_finish(m):
    try:
        pwd = ''.join(random.choices(string.ascii_letters + string.digits, k=12))
        # Формат: Пароль, TG_ID, Ник, Баланс, Должность
        sheet.append_row([pwd, str(m.from_user.id), m.text, "0", "Игрок"])
        bot.send_message(m.chat.id, f"✅ Регистрация завершена!\nВаш уникальный ID: `{pwd}`")
    except:
        bot.send_message(m.chat.id, "❌ Ошибка при сохранении в таблицу.")

# --- 6. АДМИН-ПАНЕЛЬ И СНЯТИЕ ---

@bot.message_handler(func=lambda m: m.text == "⚙️ Админ-панель")
def admin_panel(m):
    if m.from_user.id not in ADMIN_LIST: return
    kb = telebot.types.InlineKeyboardMarkup()
    kb.add(telebot.types.InlineKeyboardButton("📊 Статистика", callback_data="adm_stats"))
    kb.add(telebot.types.InlineKeyboardButton("📢 Рассылка", callback_data="adm_broadcast"))
    bot.send_message(m.chat.id, "🛠 **Панель администратора**", reply_markup=kb)

@bot.callback_query_handler(func=lambda c: True)
def callback_handler(c):
    if c.data == "pre_withdraw":
        msg = bot.send_message(c.message.chat.id, "Введите сумму для снятия:")
        bot.register_next_step_handler(msg, withdraw_request_admin)
    
    elif c.data.startswith("appr_"):
        # Одобрение выплаты админом
        _, r_idx, amt = c.data.split("_")
        confirm_payout(c, int(r_idx), float(amt))
        
    elif c.data == "adm_stats":
        count = len(sheet.get_all_values()) - 1
        bot.send_message(c.message.chat.id, f"📊 Всего игроков в базе: {count}")

    bot.answer_callback_query(c.id)

def withdraw_request_admin(m):
    try:
        amt = float(m.text)
        cell = sheet.find(str(m.from_user.id), in_column=2)
        kb = telebot.types.InlineKeyboardMarkup()
        kb.add(telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"appr_{cell.row}_{amt}"))
        
        for adm in ADMIN_LIST:
            bot.send_message(adm, f"🚨 **ЗАЯВКА НА ВЫВОД**\nИгрок: {m.from_user.first_name}\nСумма: {amt} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Заявка отправлена администраторам.")
    except:
        bot.send_message(m.chat.id, "❌ Ошибка. Введите число.")

def confirm_payout(c, row_idx, amount):
    try:
        row = sheet.row_values(row_idx)
        current_bal = float(row[3].replace(',', '.'))
        new_bal = current_bal - amount
        
        sheet.update_cell(row_idx, 4, str(new_bal))
        bot.edit_message_text(f"✅ Выплата {amount} Gold игроку {row[2]} подтверждена.", c.message.chat.id, c.message.message_id)
        bot.send_message(row[1], f"✅ Твой запрос на снятие {amount} Gold одобрен!")
    except:
        bot.send_message(c.message.chat.id, "❌ Ошибка при обновлении таблицы.")

# --- 7. ЗАПУСК И ПРЕДОТВРАЩЕНИЕ КОНФЛИКТОВ ---

app = Flask(__name__)
@app.route('/')
def health_check(): return "OK", 200

if __name__ == "__main__":
    # Запуск веб-сервера для Koyeb
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    
    # Решение ошибки 409 Conflict: удаляем вебхук перед стартом
    bot.remove_webhook()
    time.sleep(1) # Даем Telegram время закрыть старые сессии
    
    # Уведомление о запуске
    try:
        bot.send_message(NOTIFY_USER_ID, "🚀 **Бот успешно перезапущен и готов к работе!**", parse_mode="Markdown")
        print("🚀 Бот запущен")
    except:
        print("⚠️ Не удалось отправить уведомление о запуске")

    # Бесконечный цикл с игнорированием старых сообщений (skip_pending)
    bot.infinity_polling(none_stop=True, skip_pending=True)
