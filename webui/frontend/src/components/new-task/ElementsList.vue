<template>
  <v-card :elevation='0'>
    <v-card-title>
      {{ cardTitle }}
    </v-card-title>
    <v-card-subtitle v-if="cardSubtitle !== ''" align='left'>
      {{ cardSubtitle }}
    </v-card-subtitle>
    <v-card-text>
      <v-expansion-panels>
        <v-expansion-panel
          v-for="userElement in userElementsComputed"
          :key="userElement.ID + '_' + $uuid.v1()"
        >
          <v-expansion-panel-header v-slot="{ open }">
            <v-row no-gutters>
              <v-col cols="7">{{ userElement.name }}</v-col>
              <v-col
                cols="5"
                class="caption text--secondary text--darken-2"
              >
                <v-fade-transition leave-absolute>
                  <span
                    v-if="open"
                    key="0"
                  >
                    Select the arguments
                  </span>
                  <span
                    v-else
                    key="1"
                  >
                    Arguments selected: 0/{{ userElement.args.length }}
                  </span>
                </v-fade-transition>
              </v-col>
            </v-row>
          </v-expansion-panel-header>
          <v-expansion-panel-content>
            <v-card :elevation='0'>
              <v-card-text>
                <v-expansion-panels>
                  <v-expansion-panel v-for="arg in userElement.args" :key="arg.ID">
                    <v-expansion-panel-header>
                      {{ arg.name }}
                    </v-expansion-panel-header>
                    <v-expansion-panel-content class="text--secondary text--darken-2">
                      {{ arg.description }}
                      <app-adaptative-arg
                        :content='arg.content'
                        :argType="arg.contentType"
                        @changed='arg.content = $event'
                      />
                    </v-expansion-panel-content>
                  </v-expansion-panel>
                </v-expansion-panels>
              </v-card-text>
            </v-card>
          </v-expansion-panel-content>
        </v-expansion-panel>
      </v-expansion-panels>
      <v-row justify='center'>
        <v-btn color='green' @click="openSelector" text icon>
          <v-icon>mdi-plus</v-icon>
        </v-btn>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script>
import AdaptativeArgSelector from './AdaptativeArgSelector.vue'

export default {
  props: {
    cardTitle: {
      type: String,
      required: true
    },
    cardSubtitle: {
      type: String,
      required: false,
      default: ''
    },
    userElements: {
      type: Array,
      required: true
    }
  },
  computed: {
    userElementsComputed: {
      get () {
        return this.userElements
      },
      set (newValue) {
        this.$emit('modified', newValue)
      }
    }
  },
  methods: {
    openSelector () {
      this.$emit('open-selector')
    }
  },
  components: {
    appAdaptativeArg: AdaptativeArgSelector
  }
}
</script>

<style lang="scss" scoped>

</style>
