<template>
<div>
  <v-text-field
    v-if="
      argType == 'url' ||
      argType == 'path' ||
      argType == 'number' ||
      argType == 'any'
    "
    :type='argType == "number"? "number":"text"'
    v-model.lazy='argContent'
    :rules='[
      rules.emptyNotAllowed
    ]'
    required
  />
  <v-textarea
    v-if="
      argType == 'text' ||
      argType == 'json'
    "
    v-model.lazy='argContent'
    :rules='[
      rules.emptyNotAllowed
    ]'
    clear-icon='cancel'
    filled
    auto-grow
    clearable
  />
  <v-date-picker
    v-if="argType == 'date'"
    v-model.lazy="argContent"
    :min='getDate()'
    :rules='[
      rules.emptyNotAllowed
    ]'
  />
  <v-time-picker
    v-if='argType == "time"'
    v-model.lazy="argContent"
    :rules='[
      rules.emptyNotAllowed
    ]'
  />
  <v-checkbox
    v-if='argType == "boolean"'
    v-model="argContent"
    :rules='[
      rules.emptyNotAllowed
    ]'
  />
</div>
</template>

<script>
export default {
  props: {
    content: {
      type: String,
      required: true
    },
    argType: {
      type: String,
      required: true
    }
  },
  data () {
    return {
      rules: {
        emptyNotAllowed: v => !!v || 'Empty args are not allowed'
      },

      datePickerModel: null,
      timePickerModel: null
    }
  },
  computed: {
    argContent: {
      get () {
        return this.content
      },
      set (newValue) {
        this.$emit('changed', newValue)
      }
    }
  },
  methods: {
    getDate () {
      const today = new Date()
      const dd = String(today.getDate()).padStart(2, '0')
      const mm = String(today.getMonth() + 1).padStart(2, '0') // January is 0!
      const yyyy = today.getFullYear()

      return yyyy + '-' + mm + '-' + dd
    }
  }
}
</script>
