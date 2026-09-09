<template>
  <div class="container">
    <div class="mb-3">
      <label for="urls" class="form-label">List urls</label>
      <textarea
        v-model="urlText"
        class="form-control"
        id="urls"
        rows="8"
        placeholder="https://example.com&#10;https://example.org"
        :disabled="submitting"
        @keydown.ctrl.enter="submit"
        @keydown.meta.enter="submit"
      ></textarea>
      <div class="form-text">
        One URL per line, or separated by spaces or commas. Each must start with
        http:// or https://. Press Ctrl+Enter to submit.
      </div>
    </div>

    <div v-if="invalid.length" class="alert alert-warning">
      <strong>These lines were skipped:</strong>
      <ul class="mb-0">
        <li v-for="(line, key) in invalid" :key="key">{{ line }}</li>
      </ul>
    </div>

    <button
      type="button"
      class="btn btn-primary"
      :disabled="submitting || !urls.length"
      @click="submit"
    >
      <span
        v-if="submitting"
        class="spinner-border spinner-border-sm me-2"
        role="status"
        aria-hidden="true"
      ></span>
      {{ submitting ? "Submitting..." : submitLabel }}
    </button>
  </div>
</template>

<script>
import { computed, ref } from "vue";
import axios from "axios";
import { useToast } from "vue-toastification";

export default {
  setup() {
    const urlText = ref("");
    const submitting = ref(false);
    const toast = useToast();

    // split on newlines, commas and whitespace so pasted lists work as-is
    const lines = computed(() =>
      urlText.value
        .split(/[\s,]+/)
        .map((line) => line.trim())
        .filter((line) => line.length > 0)
    );

    const isValid = (line) => /^https?:\/\/.+/i.test(line);

    const urls = computed(() => lines.value.filter(isValid));
    const invalid = computed(() => lines.value.filter((l) => !isValid(l)));

    const submitLabel = computed(() =>
      urls.value.length > 1 ? `Submit ${urls.value.length} URLs` : "Submit"
    );

    const submit = async () => {
      if (submitting.value || !urls.value.length) {
        return;
      }

      submitting.value = true;
      try {
        const res = await axios.post(
          `${import.meta.env.VITE_URL || ""}/api/screenshot/v2`,
          { urls: urls.value }
        );

        if (res.data?.error) {
          toast.error(formatError(res.data.error));
        } else {
          toast.success(
            `Queued ${urls.value.length} URL${urls.value.length > 1 ? "s" : ""}`
          );
          urlText.value = "";
        }
      } catch (err) {
        // the api answers with a 400 and an error payload on bad input,
        // which axios surfaces as a rejection
        toast.error(
          formatError(err.response?.data?.error) || err.message || "Unknown error"
        );
      } finally {
        submitting.value = false;
      }
    };

    // the api reports errors as either a string or a list of strings
    const formatError = (error) => {
      if (!error) return "";
      return Array.isArray(error) ? error.join(", ") : String(error);
    };

    return {
      urlText,
      submitting,
      urls,
      invalid,
      submitLabel,
      submit,
    };
  },
};
</script>
