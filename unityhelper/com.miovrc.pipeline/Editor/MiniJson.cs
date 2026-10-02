// A small JSON reader / writer, so the package needs no other assembly.
// Objects are Dictionary<string, object>, arrays List<object>, numbers double, plus string, bool and null.
using System;
using System.Collections;
using System.Collections.Generic;
using System.Globalization;
using System.Text;

namespace MioVRCA.Pipeline
{
    internal static class MiniJson
    {
        public static object Parse(string json)
        {
            if (json == null) return null;
            int i = 0;
            object v = ReadValue(json, ref i);
            SkipSpace(json, ref i);
            if (i != json.Length) throw new FormatException("JSON: text after the end at " + i);
            return v;
        }

        static void SkipSpace(string s, ref int i)
        {
            while (i < s.Length && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n' || s[i] == '﻿')) i++;
        }

        static object ReadValue(string s, ref int i)
        {
            SkipSpace(s, ref i);
            if (i >= s.Length) throw new FormatException("JSON: unexpected end");
            char c = s[i];
            if (c == '{')
            {
                var d = new Dictionary<string, object>();
                i++;
                SkipSpace(s, ref i);
                if (i < s.Length && s[i] == '}') { i++; return d; }
                while (true)
                {
                    SkipSpace(s, ref i);
                    string k = ReadString(s, ref i);
                    SkipSpace(s, ref i);
                    if (i >= s.Length || s[i] != ':') throw new FormatException("JSON: ':' expected at " + i);
                    i++;
                    d[k] = ReadValue(s, ref i);
                    SkipSpace(s, ref i);
                    if (i < s.Length && s[i] == ',') { i++; continue; }
                    if (i < s.Length && s[i] == '}') { i++; return d; }
                    throw new FormatException("JSON: ',' or '}' expected at " + i);
                }
            }
            if (c == '[')
            {
                var l = new List<object>();
                i++;
                SkipSpace(s, ref i);
                if (i < s.Length && s[i] == ']') { i++; return l; }
                while (true)
                {
                    l.Add(ReadValue(s, ref i));
                    SkipSpace(s, ref i);
                    if (i < s.Length && s[i] == ',') { i++; continue; }
                    if (i < s.Length && s[i] == ']') { i++; return l; }
                    throw new FormatException("JSON: ',' or ']' expected at " + i);
                }
            }
            if (c == '"') return ReadString(s, ref i);
            if (string.CompareOrdinal(s, i, "true", 0, 4) == 0) { i += 4; return true; }
            if (string.CompareOrdinal(s, i, "false", 0, 5) == 0) { i += 5; return false; }
            if (string.CompareOrdinal(s, i, "null", 0, 4) == 0) { i += 4; return null; }
            int start = i;
            while (i < s.Length && ("+-0123456789.eE".IndexOf(s[i]) >= 0)) i++;
            if (i == start) throw new FormatException("JSON: unexpected '" + c + "' at " + i);
            return double.Parse(s.Substring(start, i - start), NumberStyles.Float, CultureInfo.InvariantCulture);
        }

        static string ReadString(string s, ref int i)
        {
            if (i >= s.Length || s[i] != '"') throw new FormatException("JSON: string expected at " + i);
            i++;
            var sb = new StringBuilder();
            while (true)
            {
                if (i >= s.Length) throw new FormatException("JSON: string not closed");
                char c = s[i++];
                if (c == '"') return sb.ToString();
                if (c != '\\') { sb.Append(c); continue; }
                if (i >= s.Length) throw new FormatException("JSON: string not closed");
                char e = s[i++];
                switch (e)
                {
                    case 'n': sb.Append('\n'); break;
                    case 't': sb.Append('\t'); break;
                    case 'r': sb.Append('\r'); break;
                    case 'b': sb.Append('\b'); break;
                    case 'f': sb.Append('\f'); break;
                    case 'u':
                        if (i + 4 > s.Length) throw new FormatException("JSON: bad \\u");
                        sb.Append((char)int.Parse(s.Substring(i, 4), NumberStyles.HexNumber, CultureInfo.InvariantCulture));
                        i += 4;
                        break;
                    default: sb.Append(e); break; // \" \\ \/
                }
            }
        }

        public static string Write(object v)
        {
            var sb = new StringBuilder();
            WriteValue(sb, v);
            return sb.ToString();
        }

        static void WriteValue(StringBuilder sb, object v)
        {
            if (v == null) { sb.Append("null"); return; }
            if (v is string) { WriteString(sb, (string)v); return; }
            if (v is bool) { sb.Append((bool)v ? "true" : "false"); return; }
            if (v is IDictionary)
            {
                sb.Append('{');
                bool first = true;
                foreach (DictionaryEntry e in (IDictionary)v)
                {
                    if (!first) sb.Append(',');
                    first = false;
                    WriteString(sb, Convert.ToString(e.Key, CultureInfo.InvariantCulture));
                    sb.Append(':');
                    WriteValue(sb, e.Value);
                }
                sb.Append('}');
                return;
            }
            if (v is IEnumerable)
            {
                sb.Append('[');
                bool first = true;
                foreach (object e in (IEnumerable)v)
                {
                    if (!first) sb.Append(',');
                    first = false;
                    WriteValue(sb, e);
                }
                sb.Append(']');
                return;
            }
            if (v is float || v is double || v is decimal)
            {
                double d = Convert.ToDouble(v, CultureInfo.InvariantCulture);
                if (double.IsNaN(d) || double.IsInfinity(d)) d = 0;
                sb.Append(d.ToString("R", CultureInfo.InvariantCulture));
                return;
            }
            if (v is IConvertible && !(v is char))
            {
                sb.Append(Convert.ToInt64(v, CultureInfo.InvariantCulture).ToString(CultureInfo.InvariantCulture));
                return;
            }
            WriteString(sb, v.ToString());
        }

        static void WriteString(StringBuilder sb, string s)
        {
            sb.Append('"');
            foreach (char c in s)
            {
                switch (c)
                {
                    case '"': sb.Append("\\\""); break;
                    case '\\': sb.Append("\\\\"); break;
                    case '\n': sb.Append("\\n"); break;
                    case '\r': sb.Append("\\r"); break;
                    case '\t': sb.Append("\\t"); break;
                    default:
                        if (c < 0x20) sb.Append("\\u").Append(((int)c).ToString("x4", CultureInfo.InvariantCulture));
                        else sb.Append(c);
                        break;
                }
            }
            sb.Append('"');
        }
    }

    // Reading request arguments without casts everywhere.
    internal static class J
    {
        public static Dictionary<string, object> Obj(object o) { return o as Dictionary<string, object> ?? new Dictionary<string, object>(); }
        public static List<object> Arr(object o) { return o as List<object> ?? new List<object>(); }
        public static object Get(Dictionary<string, object> d, string k) { object v; return d != null && d.TryGetValue(k, out v) ? v : null; }
        public static string Str(Dictionary<string, object> d, string k, string def = "") { var v = Get(d, k) as string; return v ?? def; }
        public static bool Bool(Dictionary<string, object> d, string k, bool def = false) { var v = Get(d, k); return v is bool ? (bool)v : def; }
        public static double Num(Dictionary<string, object> d, string k, double def = 0) { var v = Get(d, k); return v is double ? (double)v : def; }
        public static List<string> Strs(Dictionary<string, object> d, string k)
        {
            var o = new List<string>();
            var v = Get(d, k);
            if (v is string) { o.Add((string)v); return o; }
            foreach (object e in Arr(v)) if (e is string) o.Add((string)e);
            return o;
        }
    }
}
