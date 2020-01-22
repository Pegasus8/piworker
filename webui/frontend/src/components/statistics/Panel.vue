<template>
  <v-col cols='12' lg='6'>
    <v-card>
      <v-card-title>
        {{ title }}
      </v-card-title>
      <v-card-text>
        <div v-for="item in items" :key="item.title" class="text-left">
          <span>
            {{ item.title }}:
            <span class="text-info font-weight-bold">{{ item.value }}</span>
          </span>
        </div>
      </v-card-text>
    </v-card>
  </v-col>
</template>

<script>
export default {
  props: {
    title: {
      type: String,
      required: true
    },
    items: {
      type: Array,
      required: true
    }
  },
  data () {
    return {
      show: false
    }
  },
  methods: {
  }
}
</script>

<style lang="scss" scoped>
li {
  list-style: none;
}
</style>
